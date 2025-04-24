package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib" // Import pgx stdlib driver
	"github.com/stretchr/testify/assert"
)

var testDB *sql.DB
var router http.Handler // Gin engine implements http.Handler

// TestMain sets up the test environment
func TestMain(m *testing.M) {
	// Setup: Initialize database connection for tests
	connStr := os.Getenv("TEST_DB_CONNECTION")
	if connStr == "" {
		connStr = os.Getenv("DB_CONNECTION")
		if connStr == "" {
			connStr = "postgres://postgres:postgres@localhost:5432/albumdb?sslmode=disable"
		}
		log.Println("WARNING: Using main database for testing. Ensure it's clean or use TEST_DB_CONNECTION env var.")
	}

	var err error
	testDB, err = sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to test database: %v", err)
	}
	if err = testDB.Ping(); err != nil {
		log.Fatalf("Could not ping test database: %v", err)
	}

	// Assign the test DB to the global var used by handlers
	db = testDB

	// Ensure the necessary tables exist in the test DB
	initDB()                   // Create inventory table
	initProcessedOrdersTable() // Create processed_orders table

	// Set up the Gin router for testing
	gin.SetMode(gin.TestMode)
	r := setupRouter() // Use the same router setup logic as main
	router = r

	// Run tests
	exitCode := m.Run()

	// Teardown: Clean up database and close connection
	cleanupInventoryDB()
	testDB.Close()

	os.Exit(exitCode)
}

// setupRouter configures the Gin router with routes and middleware (mirrors main.go)
func setupRouter() *gin.Engine {
	router := gin.New() // Use New for tests

	api := router.Group("/api")
	{
		inventory := api.Group("/inventory")
		{
			inventory.GET("/:albumId", getInventory)

			adminRoutes := inventory.Group("")
			adminRoutes.Use(requireAdmin())
			{
				adminRoutes.GET("", getAllInventory)
				adminRoutes.PUT("/:albumId", updateInventory)
			}
		}
	}
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return router
}

// Helper to clean up the inventory table after tests
func cleanupInventoryDB() {
	_, err := testDB.Exec("DELETE FROM inventory")
	if err != nil {
		log.Printf("Failed to clean inventory table: %v", err)
	}
	// Also clean processed orders table if tests might affect it
	// _, err = testDB.Exec("DELETE FROM processed_orders")
	// if err != nil {
	// 	 log.Printf("Failed to clean processed_orders table: %v", err)
	// }
}

// --- Integration Tests ---

// Test GET /api/inventory/:albumId
func TestGetInventoryHandler_Found(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	// Insert test data
	testAlbumID := "album123"
	expectedQuantity := 10
	_, err := testDB.Exec(`INSERT INTO inventory (album_id, quantity_available, last_updated) VALUES ($1, $2, NOW())`, testAlbumID, expectedQuantity)
	assert.NoError(t, err, "Failed to insert test inventory")

	req, _ := http.NewRequest("GET", "/api/inventory/"+testAlbumID, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var inv Inventory
	err = json.Unmarshal(rr.Body.Bytes(), &inv)
	assert.NoError(t, err)
	assert.Equal(t, testAlbumID, inv.AlbumID)
	assert.Equal(t, expectedQuantity, inv.QuantityAvailable)
}

func TestGetInventoryHandler_NotFound(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	testAlbumID := "nonexistent-album"
	req, _ := http.NewRequest("GET", "/api/inventory/"+testAlbumID, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Expect 200 OK because the handler returns 0 quantity for non-existent albums
	assert.Equal(t, http.StatusOK, rr.Code)

	var inv Inventory
	err := json.Unmarshal(rr.Body.Bytes(), &inv)
	assert.NoError(t, err)
	assert.Equal(t, testAlbumID, inv.AlbumID)
	assert.Equal(t, 0, inv.QuantityAvailable) // Expect 0 quantity
	assert.WithinDuration(t, time.Now(), inv.LastUpdated, 5*time.Second) // Check timestamp is recent
}

// Test GET /api/inventory
func TestGetAllInventoryHandler_Empty(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	req, _ := http.NewRequest("GET", "/api/inventory", nil)
	req.Header.Set("Client-Type", "admin") // Required header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var invList []Inventory
	err := json.Unmarshal(rr.Body.Bytes(), &invList)
	assert.NoError(t, err)
	assert.Len(t, invList, 0)
}

func TestGetAllInventoryHandler_WithData(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	// Insert test data
	data := []struct { id string; qty int } {
		{"albumA", 5},
		{"albumB", 15},
	}
	for _, item := range data {
		_, err := testDB.Exec(`INSERT INTO inventory (album_id, quantity_available, last_updated) VALUES ($1, $2, NOW())`, item.id, item.qty)
		assert.NoError(t, err)
	}

	req, _ := http.NewRequest("GET", "/api/inventory", nil)
	req.Header.Set("Client-Type", "admin") // Required header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var invList []Inventory
	err := json.Unmarshal(rr.Body.Bytes(), &invList)
	assert.NoError(t, err)
	assert.Len(t, invList, 2)

	// Check content (map for easier lookup)
	invMap := make(map[string]int)
	for _, inv := range invList {
		invMap[inv.AlbumID] = inv.QuantityAvailable
	}
	assert.Equal(t, data[0].qty, invMap[data[0].id])
	assert.Equal(t, data[1].qty, invMap[data[1].id])
}

func TestGetAllInventoryHandler_Forbidden(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	req, _ := http.NewRequest("GET", "/api/inventory", nil)
	req.Header.Set("Client-Type", "user") // Non-admin header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)

	var response map[string]string
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "Forbidden")
}

// Test POST /api/inventory -> PUT /api/inventory/:albumId
func TestUpdateInventoryHandler_Success_New(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	albumID := "newAlbum1"
	payload := UpdateInventoryRequest{
		QuantityAvailable: 25,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PUT", "/api/inventory/"+albumID, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "admin") // Required header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var respInv Inventory
	err := json.Unmarshal(rr.Body.Bytes(), &respInv)
	assert.NoError(t, err)
	assert.Equal(t, albumID, respInv.AlbumID)
	assert.Equal(t, payload.QuantityAvailable, respInv.QuantityAvailable)
	assert.WithinDuration(t, time.Now(), respInv.LastUpdated, 5*time.Second)

	// Verify DB
	var dbQty int
	err = testDB.QueryRow("SELECT quantity_available FROM inventory WHERE album_id = $1", albumID).Scan(&dbQty)
	assert.NoError(t, err)
	assert.Equal(t, payload.QuantityAvailable, dbQty)
}

func TestUpdateInventoryHandler_Success_Update(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	// Insert initial data
	initialAlbumID := "updateAlbum1"
	initialQty := 10
	_, err := testDB.Exec(`INSERT INTO inventory (album_id, quantity_available, last_updated) VALUES ($1, $2, NOW())`, initialAlbumID, initialQty)
	assert.NoError(t, err)

	// Update payload
	payload := UpdateInventoryRequest{
		QuantityAvailable: 50,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PUT", "/api/inventory/"+initialAlbumID, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "admin") // Required header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var respInv Inventory
	err = json.Unmarshal(rr.Body.Bytes(), &respInv)
	assert.NoError(t, err)
	assert.Equal(t, initialAlbumID, respInv.AlbumID)
	assert.Equal(t, payload.QuantityAvailable, respInv.QuantityAvailable)
	assert.WithinDuration(t, time.Now(), respInv.LastUpdated, 5*time.Second)

	// Verify DB
	var dbQty int
	err = testDB.QueryRow("SELECT quantity_available FROM inventory WHERE album_id = $1", initialAlbumID).Scan(&dbQty)
	assert.NoError(t, err)
	assert.Equal(t, payload.QuantityAvailable, dbQty)
}

func TestUpdateInventoryHandler_BadRequest(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	albumID := "bad1"
	invalidPayload := []byte(`{"quantityAvailable": "not-a-number"}`)

	req, _ := http.NewRequest("PUT", "/api/inventory/"+albumID, bytes.NewBuffer(invalidPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "admin") // Required header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdateInventoryHandler_Forbidden(t *testing.T) {
	cleanupInventoryDB()
	defer cleanupInventoryDB()

	albumID := "forbiddenAlbum"
	payload := UpdateInventoryRequest{
		QuantityAvailable: 1,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PUT", "/api/inventory/"+albumID, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "user") // Non-admin header
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)

	// Verify DB was not changed (i.e., the record was never created)
	var queriedAlbumID string
	err := testDB.QueryRow("SELECT album_id FROM inventory WHERE album_id = $1", albumID).Scan(&queriedAlbumID)
	assert.ErrorIs(t, err, sql.ErrNoRows, "Querying the forbidden album ID should return sql.ErrNoRows")
} 