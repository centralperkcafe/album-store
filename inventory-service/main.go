// inventory-service main.go (Gin version)

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings" // Import strings package
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib" // Import pgx stdlib driver
	"github.com/segmentio/kafka-go"    // Import kafka-go

	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// kafka-go should be implicitly imported via kafka_consumer.go
const orderFailedTopic = "order-failed"
const orderSucceededTopic = "order-succeeded" // New topic name

var (
	db *sql.DB
	kafkaFailedEventWriter    *kafka.Writer
	kafkaSucceededEventWriter *kafka.Writer
)

// Inventory represents an item in the inventory database
type Inventory struct {
	AlbumID           string    `json:"albumId"`
	QuantityAvailable int       `json:"quantityAvailable"`
	LastUpdated       time.Time `json:"lastUpdated"`
}

// UpdateInventoryRequest represents a request to update inventory
type UpdateInventoryRequest struct {
	QuantityAvailable int `json:"quantityAvailable" binding:"required"`
}

func main() {
	// Initialize OpenTelemetry
	cleanupFunc, err := setupTracing()
	if err != nil {
		log.Printf("Failed to setup tracing: %v", err)
		// Continue running even if tracing setup fails
	} else {
		// Ensure cleanup on application shutdown
		defer func() {
			if err := cleanupFunc(context.Background()); err != nil {
				log.Printf("Failed to cleanup tracing: %v", err)
			}
		}()
		log.Println("OpenTelemetry tracing initialized successfully")
	}

	// Initialize database connection
	connStr := os.Getenv("DB_CONNECTION")
	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/albumdb?sslmode=disable"
	}
	
	db, err = sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Check connection
	err = db.Ping()
	if err != nil {
		log.Fatalf("Could not ping database: %v", err)
	}
	log.Println("Successfully connected to database")
	
	// Create tables if they don't exist
	initDB()
	initProcessedOrdersTable() // Assuming this is defined in kafka_consumer.go or elsewhere
	log.Println("Database tables initialized")

	// Initialize Kafka Consumers and Producer
	kafkaBroker := os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		kafkaBroker = "localhost:9092"
		log.Println("KAFKA_BROKER environment variable not set, using default:", kafkaBroker)
	}
	// Strip protocol prefix if present (needed for kafka-go TCP address)
	if strings.Contains(kafkaBroker, "://") {
		parts := strings.SplitN(kafkaBroker, "://", 2)
		if len(parts) > 1 {
			kafkaBroker = parts[1]
		}
	}

	// Start Kafka consumer for order creation events
	log.Printf("Starting order creation event consumer for broker: %s", kafkaBroker)
	go startOrderConsumer(kafkaBroker) // Consumer for order-created topic

	// Start Kafka consumer for album created events
	log.Printf("Starting album created event consumer for broker: %s", kafkaBroker)
	go startAlbumCreatedConsumer(kafkaBroker) // Consumer for album-created topic

	// Initialize Kafka Writer for order-failed events
	kafkaFailedEventWriter = &kafka.Writer{
		Addr:         kafka.TCP(kafkaBroker),
		Topic:        orderFailedTopic,
		Balancer:     &kafka.LeastBytes{},
		WriteTimeout: 10 * time.Second,
	}
	log.Printf("Kafka writer initialized for failed orders topic '%s' on broker '%s'", orderFailedTopic, kafkaBroker)

	// Initialize Kafka Writer for order-succeeded events
	kafkaSucceededEventWriter = &kafka.Writer{
		Addr:         kafka.TCP(kafkaBroker),
		Topic:        orderSucceededTopic,
		Balancer:     &kafka.LeastBytes{},
		WriteTimeout: 10 * time.Second,
	}
	log.Printf("Kafka writer initialized for succeeded orders topic '%s' on broker '%s'", orderSucceededTopic, kafkaBroker)

	// Defer closing the writers
	defer func() {
		log.Println("Closing Kafka writer for failed orders...")
		if err := kafkaFailedEventWriter.Close(); err != nil {
			log.Printf("Failed to close Kafka failed orders writer: %v", err)
		}
		log.Println("Closing Kafka writer for succeeded orders...")
		if err := kafkaSucceededEventWriter.Close(); err != nil {
			log.Printf("Failed to close Kafka succeeded orders writer: %v", err)
		}
	}()

	// Initialize Gin router
	router := gin.Default()

	router.Use(otelgin.Middleware("inventory-service"))
	
	// --- Routes ---
	api := router.Group("/api")
	{
		inventory := api.Group("/inventory")
		{
			inventory.GET("/:albumId", wrapHandlerWithTracing(getInventory, "getInventory")) // Publicly accessible

			// Routes requiring admin privileges
			adminRoutes := inventory.Group("")
			adminRoutes.Use(requireAdmin()) // Apply admin check middleware
			{
				adminRoutes.GET("", wrapHandlerWithTracing(getAllInventory, "getAllInventory")) // GET /api/inventory (all)
				adminRoutes.PUT("/:albumId", wrapHandlerWithTracing(updateInventory, "updateInventory")) // PUT /api/inventory/:albumId (Updated)
			}
		}
	}
	
	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Start server
	port := os.Getenv("SERVICE_PORT")
	if port == "" {
		port = "8081"
	}
	
	fmt.Printf("Inventory Service (Gin) starting on port %s\n", port)
	err = router.Run(":" + port)
	if err != nil {
		log.Fatalf("Failed to start Gin server: %v", err)
	}
}

func initDB() {
	// Create inventory table
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS inventory (
		album_id VARCHAR(50) PRIMARY KEY,
		quantity_available INTEGER NOT NULL DEFAULT 0,
		last_updated TIMESTAMP NOT NULL DEFAULT NOW()
	)`)
	
	if err != nil {
		log.Fatalf("Could not create inventory table: %v", err)
	}
}

// --- Middleware ---

// requireAdmin checks if the Client-Type header is 'admin'
func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientType := c.GetHeader("Client-Type")
		if clientType != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Forbidden: Admin privileges required"})
		return
		}
		c.Next() // Continue to the handler
	}
}

// --- Handler Functions (using gin.Context) ---

func getAllInventory(c *gin.Context) {
	rows, err := db.Query("SELECT album_id, quantity_available, last_updated FROM inventory")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query inventory: " + err.Error()})
		return
	}
	defer rows.Close()

	inventoryList := []Inventory{}
	for rows.Next() {
		var i Inventory
		if err := rows.Scan(&i.AlbumID, &i.QuantityAvailable, &i.LastUpdated); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan inventory row: " + err.Error()})
			return
		}
		inventoryList = append(inventoryList, i)
	}

	if err = rows.Err(); err != nil { // Check for errors during iteration
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating inventory rows: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, inventoryList)
}

func getInventory(c *gin.Context) {
	albumID := c.Param("albumId")

	var i Inventory
	err := db.QueryRow("SELECT album_id, quantity_available, last_updated FROM inventory WHERE album_id = $1", albumID).
		Scan(&i.AlbumID, &i.QuantityAvailable, &i.LastUpdated)
	
	if err != nil {
		if err == sql.ErrNoRows {
			// If inventory record doesn't exist, return 0 quantity
			i = Inventory{
				AlbumID:           albumID,
				QuantityAvailable: 0,
				LastUpdated:       time.Now(),
			}
			c.JSON(http.StatusOK, i) // Return the zero-value inventory
			return
		}
		// Handle other potential errors
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, i)
}

func updateInventory(c *gin.Context) {
	albumIDFromPath := c.Param("albumId") // Get albumId from URL path
	if albumIDFromPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing albumId in URL path"})
		return
	}

	var req UpdateInventoryRequest // Use the new request struct
	// Bind JSON request body to the new struct
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Set the AlbumID from the path parameter, ignoring any value from the body
	// i.AlbumID = albumIDFromPath // No longer needed as we use albumIDFromPath directly
	currentTime := time.Now() // Use a consistent time

	_, err := db.Exec(
		`INSERT INTO inventory (album_id, quantity_available, last_updated) 
		 VALUES ($1, $2, $3) 
		 ON CONFLICT (album_id) 
		 DO UPDATE SET quantity_available = $2, last_updated = $3`,
		albumIDFromPath, req.QuantityAvailable, currentTime, // Use ID from path, quantity from req
	)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update inventory: " + err.Error()})
		return
	}

	log.Printf("Inventory updated via API for albumId: %s, quantity: %d", albumIDFromPath, req.QuantityAvailable)

	// Construct the response object based on updated data
	responseInventory := Inventory{
		AlbumID:            albumIDFromPath,
		QuantityAvailable:  req.QuantityAvailable,
		LastUpdated:        currentTime,
	}

	c.JSON(http.StatusOK, responseInventory) // Return the constructed inventory state
}

// Placeholder for publishInventoryUpdate if needed later
// func publishInventoryUpdate(i Inventory) error {
// 	 log.Printf("Placeholder: Publishing inventory update for albumId: %s, quantity: %d", i.AlbumID, i.QuantityAvailable)
// 	 return nil
// }
