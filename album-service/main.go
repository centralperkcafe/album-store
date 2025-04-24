// album-service main.go (Gin version)

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/segmentio/kafka-go"

	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Album represents a music album
type Album struct {
	ID          string  `json:"id"`
	Title       string  `json:"title" binding:"required"` // Add binding for validation
	Artist      string  `json:"artist" binding:"required"`
	Price       float64 `json:"price" binding:"required,gt=0"`
	ReleaseYear int     `json:"releaseYear" binding:"required"`
	Genre       string  `json:"genre" binding:"required"`
	InitialQuantity *int `json:"initialQuantity,omitempty" binding:"omitempty,gte=0"` // Optional initial quantity
}

// AlbumCreatedEvent represents the event published when an album is created
type AlbumCreatedEvent struct {
	AlbumID     string    `json:"albumId"`
	Title       string    `json:"title"`
	Artist      string    `json:"artist"`
	Timestamp   time.Time `json:"timestamp"` // Use time.Time for Go struct
	InitialQuantity *int `json:"initialQuantity,omitempty"` // Optional initial quantity from creation
}

var db *sql.DB
var kafkaWriter *kafka.Writer // Global Kafka writer instance

const albumCreatedTopic = "album-created" // Kafka topic name

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
		// Default connection string - consider making this more robust
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

	// Create tables if they don't exist
	initDB()

	// Initialize Kafka Writer
	kafkaBroker := os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		kafkaBroker = "localhost:9092" // Default Kafka broker
		log.Println("KAFKA_BROKER environment variable not set, using default:", kafkaBroker)
	}
	// Ensure broker address is correctly formatted (e.g., remove prefixes if any)
	if strings.Contains(kafkaBroker, "://") {
		parts := strings.SplitN(kafkaBroker, "://", 2)
		if len(parts) > 1 {
			kafkaBroker = parts[1]
		}
	}

	kafkaWriter = &kafka.Writer{
		Addr:     kafka.TCP(kafkaBroker),
		Topic:    albumCreatedTopic,
		Balancer: &kafka.LeastBytes{},
		// Add other configurations like RequiredAcks, Async, etc. if needed
		WriteTimeout: 10 * time.Second,
	}
	log.Printf("Kafka writer initialized for topic '%s' on broker '%s' with timeout %s", albumCreatedTopic, kafkaBroker, kafkaWriter.WriteTimeout)

	// Optional: Add a startup check to see if we can connect to Kafka
	// This requires creating a temporary client or using admin functions, skipping for now
	// to focus on the write path.

	defer func() {
		log.Println("Closing Kafka writer...")
		if err := kafkaWriter.Close(); err != nil {
			log.Printf("Failed to close Kafka writer: %v", err)
		}
	}()

	// Initialize Gin router
	router := gin.Default() // Using Default logger and recovery middleware

	// Add OpenTelemetry middleware
	router.Use(otelgin.Middleware("album-service"))

	// --- Routes ---
	api := router.Group("/api")
	{
		albums := api.Group("/albums")
		{
			albums.GET("", wrapHandlerWithTracing(getAllAlbums, "getAllAlbums"))
			albums.GET("/:id", wrapHandlerWithTracing(getAlbum, "getAlbum"))

			// Group routes requiring admin privileges
			adminRoutes := albums.Group("")
			adminRoutes.Use(requireAdmin()) // Apply admin check middleware
			{
				adminRoutes.POST("", wrapHandlerWithTracing(createAlbum, "createAlbum"))
				adminRoutes.PUT("/:id", wrapHandlerWithTracing(updateAlbum, "updateAlbum"))
				adminRoutes.DELETE("/:id", wrapHandlerWithTracing(deleteAlbum, "deleteAlbum"))
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
		port = "8080"
	}

	fmt.Printf("Album Service (Gin) starting on port %s\n", port)
	err = router.Run(":" + port)
	if err != nil {
		log.Fatalf("Failed to start Gin server: %v", err)
	}
}

func initDB() {
	// Create albums table (same as before)
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS albums (
		id SERIAL PRIMARY KEY,
		title VARCHAR(100) NOT NULL,
		artist VARCHAR(100) NOT NULL,
		price NUMERIC(10,2) NOT NULL,
		release_year INTEGER NOT NULL,
		genre VARCHAR(50) NOT NULL
	)`)

	if err != nil {
		log.Fatalf("Could not create albums table: %v", err)
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

func getAllAlbums(c *gin.Context) {
	rows, err := db.Query("SELECT id, title, artist, price, release_year, genre FROM albums")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query albums: " + err.Error()})
		return
	}
	defer rows.Close()

	albums := []Album{}
	for rows.Next() {
		var a Album
		var id int
		if err := rows.Scan(&id, &a.Title, &a.Artist, &a.Price, &a.ReleaseYear, &a.Genre); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan album row: " + err.Error()})
			return
		}
		a.ID = strconv.Itoa(id)
		albums = append(albums, a)
	}

	if err = rows.Err(); err != nil { // Check for errors during iteration
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error iterating album rows: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, albums)
}

func getAlbum(c *gin.Context) {
	id := c.Param("id") // Get path parameter

	var a Album
	var dbID int
	err := db.QueryRow("SELECT id, title, artist, price, release_year, genre FROM albums WHERE id = $1", id).
		Scan(&dbID, &a.Title, &a.Artist, &a.Price, &a.ReleaseYear, &a.Genre)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Album not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
		return
	}

	a.ID = strconv.Itoa(dbID)
	c.JSON(http.StatusOK, a)
}

func createAlbum(c *gin.Context) {
	// Get the current request context to obtain tracing information
	ctx := c.Request.Context()
	
	var a Album
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Create a child span for database operations
	ctx, dbSpan := tracer.Start(ctx, "db.insert_album")
	
	var id int
	err := db.QueryRowContext(ctx,
		"INSERT INTO albums (title, artist, price, release_year, genre) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		a.Title, a.Artist, a.Price, a.ReleaseYear, a.Genre,
	).Scan(&id)
	
	dbSpan.End()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create album in DB: " + err.Error()})
		return
	}

	a.ID = strconv.Itoa(id)

	// Create a child span for Kafka publishing
	ctx, kafkaSpan := tracer.Start(ctx, "kafka.publish_album_created")
	defer kafkaSpan.End()
	
	// Prepare and publish Kafka event
	event := AlbumCreatedEvent{
		AlbumID:         a.ID,
		Title:           a.Title,
		Artist:          a.Artist,
		Timestamp:       time.Now(),
		InitialQuantity: a.InitialQuantity,
	}

	// Serialize the event
	eventJSON, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshaling AlbumCreatedEvent: %v", err)
		kafkaSpan.RecordError(err)
		// Handle the error, but still return a success response since the album was created
	} else {
		// Extract trace context and add to Kafka message headers
		log.Printf("AlbumCreatedEvent JSON: %s", string(eventJSON))
		headers := InjectTraceInfoToKafkaMessage(ctx)
		
		// Send Kafka message with trace headers
		err = kafkaWriter.WriteMessages(ctx, kafka.Message{
			Key:     []byte(a.ID),
			Value:   eventJSON,
			Headers: headers,
		})
		
		if err != nil {
			log.Printf("Error publishing album created event to Kafka: %v", err)
			kafkaSpan.RecordError(err)
			// Handle the error, but still return a success response
		} else {
			log.Printf("Published album created event to Kafka for albumId: %s", a.ID)
		}
	}

	c.JSON(http.StatusCreated, a)
}

func updateAlbum(c *gin.Context) {
	id := c.Param("id")

	var a Album
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	res, err := db.Exec(
		"UPDATE albums SET title = $1, artist = $2, price = $3, release_year = $4, genre = $5 WHERE id = $6",
		a.Title, a.Artist, a.Price, a.ReleaseYear, a.Genre, id,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update album: " + err.Error()})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		// This error is less likely but possible
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get affected rows: " + err.Error()})
		return
	}

	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Album not found"})
		return
	}

	a.ID = id // Set the ID from the path parameter in the response
	c.JSON(http.StatusOK, a)
}

func deleteAlbum(c *gin.Context) {
	id := c.Param("id")

	res, err := db.Exec("DELETE FROM albums WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete album: " + err.Error()})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get affected rows: " + err.Error()})
		return
	}

	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Album not found"})
		return
	}

	c.Status(http.StatusNoContent) // Use 204 No Content for successful deletion
}
