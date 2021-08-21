package database

import (
	"database/sql"
	"errors"
	"log"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
)

var db *sql.DB

// Initialize the database and creates the schema if it doesn't already exist in the file specified
func Initialize(path string) (err error) {
	if db, err = sql.Open("sqlite", path); err != nil {
		return err
	}
	log.Println("[database][Initialize] Beginning schema migration on database with driver=sqlite")
	_, _ = db.Exec("PRAGMA foreign_keys=ON")
	if err = createSchema(); err != nil {
		_ = db.Close()
	}
	return err
}

// createSchema creates the schema required to perform all database operations.
func createSchema() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS channel (
			channel_id  VARCHAR(64) PRIMARY KEY, 
		    locked      INTEGER     DEFAULT FALSE
		)
	`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS connection (
			first_channel_id   VARCHAR(64) REFERENCES channel(channel_id), 
			second_channel_id  VARCHAR(64) REFERENCES channel(channel_id),
			UNIQUE (first_channel_id),
			UNIQUE (second_channel_id)
		)
	`)
	return err
}

func CreateConnection(firstChannelID, secondChannelID string) error {
	if err := createChannel(firstChannelID); err != nil {
		return err
	}
	if err := createChannel(secondChannelID); err != nil {
		return err
	}
	_, err := db.Exec("INSERT INTO connection (first_channel_id, second_channel_id) VALUES ($1, $2)", firstChannelID, secondChannelID)
	return err
}

// GetOtherChannelIDFromConnection gets the other channel ID from a connection, or returns ErrNotFound if
// there is no connection with the related ID
func GetOtherChannelIDFromConnection(channelID string) (string, error) {
	rows, err := db.Query("SELECT first_channel_id, second_channel_id FROM connection WHERE first_channel_id = $1 OR second_channel_id = $1", channelID)
	if err != nil {
		return "", err
	}
	var firstChannelID, secondChannelID string
	var found bool
	for rows.Next() {
		_ = rows.Scan(&firstChannelID, &secondChannelID)
		found = true
		break
	}
	_ = rows.Close()
	if !found {
		return "", ErrNotFound
	}
	if firstChannelID == channelID {
		return secondChannelID, nil
	}
	return firstChannelID, nil
}

func DeleteConnectionByChannelID(channelID string) error {
	otherChannelID, err := GetOtherChannelIDFromConnection(channelID)
	if err != nil {
		return err
	}
	_, _ = db.Exec("DELETE FROM connection WHERE first_channel_id IN ($1, $2) AND second_channel_id IN ($1, $2)", channelID, otherChannelID)
	_, _ = db.Exec("DELETE FROM channel WHERE channel_id IN ($1, $2)", channelID, otherChannelID)
	return err
}

func createChannel(channelID string) error {
	_, err := db.Exec("INSERT INTO channel (channel_id) VALUES ($1)", channelID)
	return err
}
