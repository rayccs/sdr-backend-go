//go:build ignore

package main


import (
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type RawLog struct {
	gorm.Model
	Data string
}

func main() {
	dsn := "postgres://postgres.ulpttibobjmzctrgiiey:SdrWhastapp2026Base@aws-0-ca-central-1.pooler.supabase.com:5432/postgres"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Error connecting: %v", err)
	}

	var logs []RawLog
	if err := db.Order("created_at desc").Limit(5).Find(&logs).Error; err != nil {
		log.Fatalf("Error querying: %v", err)
	}

	for i, l := range logs {
		fmt.Printf("--- LOG %d ---\n%s\n\n", i+1, l.Data)
	}
}
