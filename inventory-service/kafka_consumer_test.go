package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
)

// TestProcessAlbumCreatedEvent tests the logic for handling AlbumCreatedEvents.
func TestProcessAlbumCreatedEvent(t *testing.T) {
	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("An error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

	// Test case 1: Success - New album, inventory initialized with quantity
	t.Run("Success - New album, inventory initialized with quantity", func(t *testing.T) {
		initialQty := 10
		event := AlbumCreatedEvent{
			AlbumID:         "album-123",
			Title:           "Test Album",
			Artist:          "Test Artist",
			Timestamp:       time.Now(),
			InitialQuantity: &initialQty,
		}
		eventBytes, _ := json.Marshal(event)
		testMsg := kafka.Message{Value: eventBytes}

		expectedSQL := `
        INSERT INTO inventory (album_id, quantity_available, last_updated)
        VALUES ($1, $2, NOW())
        ON CONFLICT (album_id) DO NOTHING`
		mock.ExpectExec(expectedSQL).
			WithArgs(event.AlbumID, initialQty).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := processAlbumCreatedEvent(mockDB, testMsg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Test case 2: Success - Album already exists, no action taken (ON CONFLICT DO NOTHING)
	t.Run("Success - Album already exists, no action taken", func(t *testing.T) {
		event := AlbumCreatedEvent{
			AlbumID:         "album-456",
			Title:           "Existing Album",
			Artist:          "Existing Artist",
			Timestamp:       time.Now(),
			InitialQuantity: nil,
		}
		eventBytes, _ := json.Marshal(event)
		testMsg := kafka.Message{Value: eventBytes}

		expectedSQL := `
        INSERT INTO inventory (album_id, quantity_available, last_updated)
        VALUES ($1, $2, NOW())
        ON CONFLICT (album_id) DO NOTHING`
		mock.ExpectExec(expectedSQL).
			WithArgs(event.AlbumID, 0).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := processAlbumCreatedEvent(mockDB, testMsg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Test case 3: Error - Database execution error
	t.Run("Error - Database execution error", func(t *testing.T) {
		initialQty := 5
		event := AlbumCreatedEvent{
			AlbumID:         "album-789",
			Title:           "DB Error Album",
			Artist:          "DB Error Artist",
			Timestamp:       time.Now(),
			InitialQuantity: &initialQty,
		}
		eventBytes, _ := json.Marshal(event)
		testMsg := kafka.Message{Value: eventBytes}

		expectedSQL := `
        INSERT INTO inventory (album_id, quantity_available, last_updated)
        VALUES ($1, $2, NOW())
        ON CONFLICT (album_id) DO NOTHING`
		dbError := fmt.Errorf("mock db connection error")
		mock.ExpectExec(expectedSQL).
			WithArgs(event.AlbumID, initialQty).
			WillReturnError(dbError)

		err := processAlbumCreatedEvent(mockDB, testMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database execution failed")
		assert.ErrorIs(t, err, dbError)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Test case 4: Error - JSON parsing error
	t.Run("Error - JSON parsing error", func(t *testing.T) {
		badMsg := kafka.Message{Value: []byte("this is not json")}

		err := processAlbumCreatedEvent(mockDB, badMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse AlbumCreatedEvent")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Success - Initial quantity is zero", func(t *testing.T) {
		initialQty := 0
		event := AlbumCreatedEvent{
			AlbumID:         "album-zero",
			Title:           "Zero Qty Album",
			Artist:          "Zero Artist",
			Timestamp:       time.Now(),
			InitialQuantity: &initialQty,
		}
		eventBytes, _ := json.Marshal(event)
		testMsg := kafka.Message{Value: eventBytes}

		expectedSQL := `
        INSERT INTO inventory (album_id, quantity_available, last_updated)
        VALUES ($1, $2, NOW())
        ON CONFLICT (album_id) DO NOTHING`
		mock.ExpectExec(expectedSQL).
			WithArgs(event.AlbumID, 0).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := processAlbumCreatedEvent(mockDB, testMsg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Success - Initial quantity is negative defaults to zero", func(t *testing.T) {
		initialQty := -10
		event := AlbumCreatedEvent{
			AlbumID:         "album-negative",
			Title:           "Negative Qty Album",
			Artist:          "Negative Artist",
			Timestamp:       time.Now(),
			InitialQuantity: &initialQty,
		}
		eventBytes, _ := json.Marshal(event)
		testMsg := kafka.Message{Value: eventBytes}

		expectedSQL := `
        INSERT INTO inventory (album_id, quantity_available, last_updated)
        VALUES ($1, $2, NOW())
        ON CONFLICT (album_id) DO NOTHING`
		mock.ExpectExec(expectedSQL).
			WithArgs(event.AlbumID, 0).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := processAlbumCreatedEvent(mockDB, testMsg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// Note: Add tests for processConfirmedOrder separately using a similar pattern,
// mocking BeginTx, ExecContext within the transaction, Commit/Rollback etc. 