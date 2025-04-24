package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	// Add kafka import for dummy writer
	"github.com/segmentio/kafka-go"

	"github.com/gin-gonic/gin" // Import Gin
	"github.com/stretchr/testify/assert"

	// _ "github.com/lib/pq" // Remove lib/pq import
	_ "github.com/jackc/pgx/v5/stdlib" // Import pgx stdlib driver
)

var testDB *sql.DB // Use a separate connection for testing if possible, or manage cleanup carefully
var router http.Handler // Router instance used for tests (Gin engine implements http.Handler)

// TestMain sets up the test environment
func TestMain(m *testing.M) {
	// Setup: Initialize database connection for tests
	connStr := os.Getenv("TEST_DB_CONNECTION")
	if connStr == "" {
		// Fallback to the same DB used by main, BUT BE CAREFUL
		// Assumes the main DB is accessible and testing won't interfere badly
		// A dedicated test database is strongly recommended!
		connStr = os.Getenv("DB_CONNECTION")
		if connStr == "" {
			connStr = "postgres://postgres:postgres@localhost:5432/albumdb?sslmode=disable" // Ensure sslmode is set if needed
		}
		log.Println("WARNING: Using main database for testing. Ensure it's clean or use TEST_DB_CONNECTION env var.")
	}

	var err error
	// Use "pgx" as the driver name for sql.Open
	testDB, err = sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to test database: %v", err)
	}
	if err = testDB.Ping(); err != nil {
		log.Fatalf("Could not ping test database: %v", err)
	}

	// Assign the test DB to the global var used by handlers (for testing)
	// NOTE: This is a simplification. Dependency injection is a better pattern.
	db = testDB

	// Ensure the table exists in the test DB
	initDB() // Uses the global 'db' which is now testDB

	// Initialize a dummy Kafka writer to prevent nil pointer dereference in tests
	// This writer won't actually publish messages effectively.
	kafkaWriter = kafka.NewWriter(kafka.WriterConfig{
		Brokers: []string{"localhost:9092"}, // Dummy broker, doesn't need to be running for tests
		Topic:   albumCreatedTopic,      // Use the constant defined in main.go
		Async:   true,                   // Use Async to prevent blocking test execution
	})
	log.Println("Initialized dummy Kafka writer for tests.")

	// Set up the Gin router for testing
	gin.SetMode(gin.TestMode) // Set Gin to Test Mode
	r := setupRouter()        // Use the same router setup logic as main
	router = r                // Assign the Gin engine to the http.Handler

	// Run tests
	exitCode := m.Run()

	// Teardown: Clean up database, close connection, close Kafka writer
	cleanupDB()
	testDB.Close()
	// Close the dummy Kafka writer
	if err := kafkaWriter.Close(); err != nil {
		log.Printf("Error closing dummy Kafka writer: %v", err)
	}

	os.Exit(exitCode)
}

// setupRouter configures the Gin router with routes and middleware (mirrors main.go)
func setupRouter() *gin.Engine {
	router := gin.New() // Use New instead of Default in tests to avoid default middleware unless needed

	api := router.Group("/api")
	{
		albums := api.Group("/albums")
		{
			albums.GET("", getAllAlbums)
			albums.GET("/:id", getAlbum)

			adminRoutes := albums.Group("")
			adminRoutes.Use(requireAdmin())
			{
				adminRoutes.POST("", createAlbum)
				adminRoutes.PUT("/:id", updateAlbum)
				adminRoutes.DELETE("/:id", deleteAlbum)
			}
		}
	}
	router.GET("/health", func(c *gin.Context) { // Add health check route for completeness if needed by tests
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return router
}

// Helper to clean up the albums table after tests
func cleanupDB() {
	_, err := testDB.Exec("DELETE FROM albums") // Simple cleanup
	if err != nil {
		log.Printf("Failed to clean albums table: %v", err)
	}
	// Reset sequence if needed (optional, depends on test requirements)
	// _, err = testDB.Exec("ALTER SEQUENCE albums_id_seq RESTART WITH 1")
	// if err != nil {
	// 	log.Printf("Failed to reset albums sequence: %v", err)
	// }
}

// --- Integration Tests ---

func TestCreateAlbumHandler_Success(t *testing.T) {
	// Ensure cleanup happens even if test fails
	defer cleanupDB()

	albumPayload := Album{
		Title:       "Test Album Title",
		Artist:      "Test Artist Name",
		Price:       19.99,
		ReleaseYear: 2023,
		Genre:       "Testing",
	}
	payloadBytes, _ := json.Marshal(albumPayload)

	req, _ := http.NewRequest("POST", "/api/albums", bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	// Set the required Client-Type header (now checked by middleware)
	req.Header.Set("Client-Type", "admin")

	rr := httptest.NewRecorder() // Response recorder
	router.ServeHTTP(rr, req)   // Use the globally configured router (Gin engine)

	// Assertions
	assert.Equal(t, http.StatusCreated, rr.Code, "Expected status code 201 Created")

	// Check the response body
	var createdAlbum Album
	err := json.Unmarshal(rr.Body.Bytes(), &createdAlbum)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.NotEmpty(t, createdAlbum.ID, "Created album ID should not be empty")
	assert.Equal(t, albumPayload.Title, createdAlbum.Title)
	assert.Equal(t, albumPayload.Artist, createdAlbum.Artist)
	assert.Equal(t, albumPayload.Price, createdAlbum.Price)
	assert.Equal(t, albumPayload.ReleaseYear, createdAlbum.ReleaseYear)
	assert.Equal(t, albumPayload.Genre, createdAlbum.Genre)

	// Verify data in the database (optional but good practice)
	var dbTitle string
	dbID, _ := strconv.Atoi(createdAlbum.ID) // Convert ID back to int for DB query
	err = testDB.QueryRow("SELECT title FROM albums WHERE id = $1", dbID).Scan(&dbTitle)
	assert.NoError(t, err, "Should find the created album in the database")
	assert.Equal(t, albumPayload.Title, dbTitle, "Album title in DB should match payload")
}

func TestCreateAlbumHandler_Forbidden(t *testing.T) {
	defer cleanupDB()

	albumPayload := Album{
		Title:       "Forbidden Album",
		Artist:      "Forbidden Artist",
		Price:       1.00,
		ReleaseYear: 2024,
		Genre:       "Forbidden",
	} // Use a valid payload now as middleware runs first
	payloadBytes, _ := json.Marshal(albumPayload)

	req, _ := http.NewRequest("POST", "/api/albums", bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	// DO NOT set Client-Type or set it to something other than "admin"
	req.Header.Set("Client-Type", "user")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code, "Expected status code 403 Forbidden")

	// Verify error message from middleware
	var response map[string]string
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err, "Should be able to unmarshal error response")
	assert.Contains(t, response["error"], "Forbidden", "Error message should indicate forbidden")

	// Verify no album was created
	var count int
	err = testDB.QueryRow("SELECT COUNT(*) FROM albums WHERE title = $1", albumPayload.Title).Scan(&count)
	assert.NoError(t, err, "DB query should succeed")
	assert.Equal(t, 0, count, "No album should have been created in the database")
}

func TestCreateAlbumHandler_BadRequest(t *testing.T) {
	defer cleanupDB()

	invalidPayload := []byte(`{"title": "Bad JSON", "artist": `) // Invalid JSON

	req, _ := http.NewRequest("POST", "/api/albums", bytes.NewBuffer(invalidPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "admin") // Need admin to reach the decoding step

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code, "Expected status code 400 Bad Request")
}

// Test creating an album with the optional initial quantity
func TestCreateAlbumHandler_WithInitialQuantity(t *testing.T) {
	cleanupDB()
	defer cleanupDB()

	initialQty := 50 // Define the initial quantity
	albumPayload := Album{
		Title:           "Album With Initial Qty",
		Artist:          "Test Artist Q",
		Price:           25.50,
		ReleaseYear:     2024,
		Genre:           "Test Q",
		InitialQuantity: &initialQty, // Use pointer for optional field
	}
	payloadBytes, _ := json.Marshal(albumPayload)

	req, _ := http.NewRequest("POST", "/api/albums", bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "admin") // Required header

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Assertions
	assert.Equal(t, http.StatusCreated, rr.Code, "Expected status code 201 Created")

	// Check the response body
	var createdAlbum Album
	err := json.Unmarshal(rr.Body.Bytes(), &createdAlbum)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.NotEmpty(t, createdAlbum.ID, "Created album ID should not be empty")
	assert.Equal(t, albumPayload.Title, createdAlbum.Title)
	assert.Equal(t, albumPayload.Artist, createdAlbum.Artist)
	// Assert InitialQuantity is present and correct in the response
	assert.NotNil(t, createdAlbum.InitialQuantity, "Response should include InitialQuantity")
	if createdAlbum.InitialQuantity != nil { // Check for nil before dereferencing
	    assert.Equal(t, initialQty, *createdAlbum.InitialQuantity, "Response InitialQuantity should match payload")
	}

	// Verify data in the database
	var dbTitle string
	dbID, _ := strconv.Atoi(createdAlbum.ID)
	err = testDB.QueryRow("SELECT title FROM albums WHERE id = $1", dbID).Scan(&dbTitle)
	assert.NoError(t, err, "Should find the created album in the database")
	assert.Equal(t, albumPayload.Title, dbTitle, "Album title in DB should match payload")
}

// Tests for GET /api/albums endpoint

func TestGetAllAlbumsHandler_Empty(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	req, _ := http.NewRequest("GET", "/api/albums", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200 OK")

	// Verify response body is an empty array
	var albums []Album
	err := json.Unmarshal(rr.Body.Bytes(), &albums)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Len(t, albums, 0, "Response should be an empty array of albums")
}

func TestGetAllAlbumsHandler_WithData(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Insert test data
	testAlbums := []Album{
		{Title: "Test Album 1", Artist: "Test Artist 1", Price: 9.99, ReleaseYear: 2020, Genre: "Test Genre 1"},
		{Title: "Test Album 2", Artist: "Test Artist 2", Price: 14.99, ReleaseYear: 2021, Genre: "Test Genre 2"},
		{Title: "Test Album 3", Artist: "Test Artist 3", Price: 19.99, ReleaseYear: 2022, Genre: "Test Genre 3"},
	}

	// Insert the test albums into the database
	for i, album := range testAlbums {
		var id int
		err := testDB.QueryRow(
			"INSERT INTO albums (title, artist, price, release_year, genre) VALUES ($1, $2, $3, $4, $5) RETURNING id",
			album.Title, album.Artist, album.Price, album.ReleaseYear, album.Genre,
		).Scan(&id)
		assert.NoError(t, err, "Failed to insert test album %d", i+1)
		testAlbums[i].ID = strconv.Itoa(id)
	}

	// Make the API request
	req, _ := http.NewRequest("GET", "/api/albums", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200 OK")

	// Verify response body has the expected albums
	var albums []Album
	err := json.Unmarshal(rr.Body.Bytes(), &albums)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Len(t, albums, len(testAlbums), "Response should contain the same number of albums inserted")

	// Verify the content (without relying on specific order)
	albumMap := make(map[string]Album)
	for _, album := range albums {
		albumMap[album.ID] = album
	}

	for _, expectedAlbum := range testAlbums {
		actualAlbum, exists := albumMap[expectedAlbum.ID]
		assert.True(t, exists, "Expected album with ID %s to be in response", expectedAlbum.ID)
		assert.Equal(t, expectedAlbum.Title, actualAlbum.Title, "Album title should match")
		assert.Equal(t, expectedAlbum.Artist, actualAlbum.Artist, "Album artist should match")
		assert.Equal(t, expectedAlbum.Price, actualAlbum.Price, "Album price should match")
		assert.Equal(t, expectedAlbum.ReleaseYear, actualAlbum.ReleaseYear, "Album release year should match")
		assert.Equal(t, expectedAlbum.Genre, actualAlbum.Genre, "Album genre should match")
	}
}

// Tests for GET /api/albums/{id} endpoint

func TestGetAlbumHandler_NotFound(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Request a non-existent album ID
	req, _ := http.NewRequest("GET", "/api/albums/999999", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusNotFound, rr.Code, "Expected status code 404 Not Found")

	// Verify error message in response
	var response map[string]string
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Contains(t, response, "error", "Response should contain an error message")
	assert.Equal(t, "Album not found", response["error"], "Error message should indicate album not found")
}

func TestGetAlbumHandler_Found(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Insert a test album
	testAlbum := Album{
		Title:       "Test Get Album",
		Artist:      "Test Get Artist",
		Price:       12.34,
		ReleaseYear: 2023,
		Genre:       "Test Get Genre",
	}

	var id int
	err := testDB.QueryRow(
		"INSERT INTO albums (title, artist, price, release_year, genre) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		testAlbum.Title, testAlbum.Artist, testAlbum.Price, testAlbum.ReleaseYear, testAlbum.Genre,
	).Scan(&id)
	assert.NoError(t, err, "Failed to insert test album")
	testAlbum.ID = strconv.Itoa(id)

	// Request the album by ID
	req, _ := http.NewRequest("GET", "/api/albums/"+testAlbum.ID, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200 OK")

	// Verify response body has the expected album
	var album Album
	err = json.Unmarshal(rr.Body.Bytes(), &album)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Equal(t, testAlbum.ID, album.ID, "Album ID should match")
	assert.Equal(t, testAlbum.Title, album.Title, "Album title should match")
	assert.Equal(t, testAlbum.Artist, album.Artist, "Album artist should match")
	assert.Equal(t, testAlbum.Price, album.Price, "Album price should match")
	assert.Equal(t, testAlbum.ReleaseYear, album.ReleaseYear, "Album release year should match")
	assert.Equal(t, testAlbum.Genre, album.Genre, "Album genre should match")
}

// Tests for PUT /api/albums/{id} endpoint

func TestUpdateAlbumHandler_Success(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Insert a test album
	originalAlbum := Album{
		Title:       "Original Title",
		Artist:      "Original Artist",
		Price:       9.99,
		ReleaseYear: 2020,
		Genre:       "Original Genre",
	}

	var id int
	err := testDB.QueryRow(
		"INSERT INTO albums (title, artist, price, release_year, genre) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		originalAlbum.Title, originalAlbum.Artist, originalAlbum.Price, originalAlbum.ReleaseYear, originalAlbum.Genre,
	).Scan(&id)
	assert.NoError(t, err, "Failed to insert test album")
	originalAlbum.ID = strconv.Itoa(id)

	// Updated album data
	updatedAlbum := Album{
		// ID is set in the URL, not the payload
		Title:       "Updated Title",
		Artist:      "Updated Artist",
		Price:       19.99,
		ReleaseYear: 2023,
		Genre:       "Updated Genre",
	}
	payloadBytes, _ := json.Marshal(updatedAlbum)

	// Make the update request
	req, _ := http.NewRequest("PUT", "/api/albums/"+originalAlbum.ID, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "admin") // Required for update permission (checked by middleware)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusOK, rr.Code, "Expected status code 200 OK")

	// Verify response body
	var responseAlbum Album
	err = json.Unmarshal(rr.Body.Bytes(), &responseAlbum)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Equal(t, originalAlbum.ID, responseAlbum.ID, "Album ID should remain the same")
	assert.Equal(t, updatedAlbum.Title, responseAlbum.Title, "Album title should be updated")
	assert.Equal(t, updatedAlbum.Artist, responseAlbum.Artist, "Album artist should be updated")
	assert.Equal(t, updatedAlbum.Price, responseAlbum.Price, "Album price should be updated")
	assert.Equal(t, updatedAlbum.ReleaseYear, responseAlbum.ReleaseYear, "Album release year should be updated")
	assert.Equal(t, updatedAlbum.Genre, responseAlbum.Genre, "Album genre should be updated")

	// Verify database was updated
	var dbAlbum Album
	var dbID int
	err = testDB.QueryRow("SELECT id, title, artist, price, release_year, genre FROM albums WHERE id = $1", originalAlbum.ID).
		Scan(&dbID, &dbAlbum.Title, &dbAlbum.Artist, &dbAlbum.Price, &dbAlbum.ReleaseYear, &dbAlbum.Genre)
	assert.NoError(t, err, "Should be able to query updated album")
	dbAlbum.ID = strconv.Itoa(dbID)
	assert.Equal(t, updatedAlbum.Title, dbAlbum.Title, "Album title in DB should be updated")
	assert.Equal(t, updatedAlbum.Artist, dbAlbum.Artist, "Album artist in DB should be updated")
	assert.Equal(t, updatedAlbum.Price, dbAlbum.Price, "Album price in DB should be updated")
	assert.Equal(t, updatedAlbum.ReleaseYear, dbAlbum.ReleaseYear, "Album release year in DB should be updated")
	assert.Equal(t, updatedAlbum.Genre, dbAlbum.Genre, "Album genre in DB should be updated")
}

func TestUpdateAlbumHandler_NotFound(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Updated album data for a non-existent album
	updatedAlbum := Album{
		Title:       "Updated Title",
		Artist:      "Updated Artist",
		Price:       19.99,
		ReleaseYear: 2023,
		Genre:       "Updated Genre",
	}
	payloadBytes, _ := json.Marshal(updatedAlbum)

	// Make the update request with a non-existent ID
	req, _ := http.NewRequest("PUT", "/api/albums/999999", bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "admin") // Required for update permission
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusNotFound, rr.Code, "Expected status code 404 Not Found")

	// Verify error message in response
	var response map[string]string
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Contains(t, response, "error", "Response should contain an error message")
	assert.Equal(t, "Album not found", response["error"], "Error message should indicate album not found")
}

func TestUpdateAlbumHandler_Forbidden(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Insert a test album
	originalAlbum := Album{
		Title:       "Original Title",
		Artist:      "Original Artist",
		Price:       9.99,
		ReleaseYear: 2020,
		Genre:       "Original Genre",
	}

	var id int
	err := testDB.QueryRow(
		"INSERT INTO albums (title, artist, price, release_year, genre) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		originalAlbum.Title, originalAlbum.Artist, originalAlbum.Price, originalAlbum.ReleaseYear, originalAlbum.Genre,
	).Scan(&id)
	assert.NoError(t, err, "Failed to insert test album")
	originalAlbum.ID = strconv.Itoa(id)

	// Updated album data
	updatedAlbum := Album{
		Title:       "Updated Title",
		Artist:      "Updated Artist",
		Price:       19.99,
		ReleaseYear: 2023,
		Genre:       "Updated Genre",
	}
	payloadBytes, _ := json.Marshal(updatedAlbum)

	// Make the update request WITHOUT admin client type
	req, _ := http.NewRequest("PUT", "/api/albums/"+originalAlbum.ID, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Type", "user") // Not an admin
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusForbidden, rr.Code, "Expected status code 403 Forbidden")

	// Verify error message in response (from middleware)
	var response map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Contains(t, response, "error", "Response should contain an error message")
	assert.Contains(t, response["error"], "Forbidden", "Error message should indicate forbidden") // Check new middleware message

	// Verify database was NOT updated
	var dbTitle string
	err = testDB.QueryRow("SELECT title FROM albums WHERE id = $1", originalAlbum.ID).Scan(&dbTitle)
	assert.NoError(t, err, "Should be able to query album")
	assert.Equal(t, originalAlbum.Title, dbTitle, "Album title should not have been updated")
}

// Tests for DELETE /api/albums/{id} endpoint

func TestDeleteAlbumHandler_Success(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Insert a test album
	testAlbum := Album{
		Title:       "Album to Delete",
		Artist:      "Delete Artist",
		Price:       9.99,
		ReleaseYear: 2020,
		Genre:       "Delete Genre",
	}

	var id int
	err := testDB.QueryRow(
		"INSERT INTO albums (title, artist, price, release_year, genre) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		testAlbum.Title, testAlbum.Artist, testAlbum.Price, testAlbum.ReleaseYear, testAlbum.Genre,
	).Scan(&id)
	assert.NoError(t, err, "Failed to insert test album")
	testAlbum.ID = strconv.Itoa(id)

	// Make the delete request
	req, _ := http.NewRequest("DELETE", "/api/albums/"+testAlbum.ID, nil)
	req.Header.Set("Client-Type", "admin") // Required for delete permission (checked by middleware)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code (should be 204 No Content now)
	assert.Equal(t, http.StatusNoContent, rr.Code, "Expected status code 204 No Content")

	// Verify the album was deleted from the database by trying to query it
	var deletedID int // Variable to scan into
	err = testDB.QueryRow("SELECT id FROM albums WHERE id = $1", testAlbum.ID).Scan(&deletedID)

	// Expect ErrNoRows because the row should be deleted
	assert.ErrorIs(t, err, sql.ErrNoRows, "Querying the deleted album ID should return sql.ErrNoRows")
}

func TestDeleteAlbumHandler_NotFound(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Make the delete request with a non-existent ID
	req, _ := http.NewRequest("DELETE", "/api/albums/999999", nil)
	req.Header.Set("Client-Type", "admin") // Required for delete permission
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusNotFound, rr.Code, "Expected status code 404 Not Found")

	// Verify error message in response
	var response map[string]string
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Contains(t, response, "error", "Response should contain an error message")
	assert.Equal(t, "Album not found", response["error"], "Error message should indicate album not found")
}

func TestDeleteAlbumHandler_Forbidden(t *testing.T) {
	// Ensure the DB is empty
	cleanupDB()
	defer cleanupDB() // Cleanup after test completes

	// Insert a test album
	testAlbum := Album{
		Title:       "Album to Not Delete",
		Artist:      "No Delete Artist",
		Price:       9.99,
		ReleaseYear: 2020,
		Genre:       "No Delete Genre",
	}

	var id int
	err := testDB.QueryRow(
		"INSERT INTO albums (title, artist, price, release_year, genre) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		testAlbum.Title, testAlbum.Artist, testAlbum.Price, testAlbum.ReleaseYear, testAlbum.Genre,
	).Scan(&id)
	assert.NoError(t, err, "Failed to insert test album")
	testAlbum.ID = strconv.Itoa(id)

	// Make the delete request WITHOUT admin client type
	req, _ := http.NewRequest("DELETE", "/api/albums/"+testAlbum.ID, nil)
	req.Header.Set("Client-Type", "user") // Not an admin
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify status code
	assert.Equal(t, http.StatusForbidden, rr.Code, "Expected status code 403 Forbidden")

	// Verify error message in response (from middleware)
	var response map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err, "Should be able to unmarshal response body")
	assert.Contains(t, response, "error", "Response should contain an error message")
	assert.Contains(t, response["error"], "Forbidden", "Error message should indicate forbidden") // Check new middleware message

	// Verify the album was NOT deleted from the database
	var count int
	err = testDB.QueryRow("SELECT COUNT(*) FROM albums WHERE id = $1", testAlbum.ID).Scan(&count)
	assert.NoError(t, err, "Should be able to query database")
	assert.Equal(t, 1, count, "Album should still exist in the database")
}