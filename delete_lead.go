//go:build ignore

package main

import (
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Lead struct {
	gorm.Model
	Phone string
}

type Conversation struct {
	gorm.Model
	LeadID uint
}

func main() {
	dsn := "postgres://postgres.ulpttibobjmzctrgiiey:SdrWhastapp2026Base@aws-0-ca-central-1.pooler.supabase.com:5432/postgres"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database")
	}

	var lead Lead
	result := db.Where("phone = ?", "56932973384").First(&lead)
	if result.Error != nil {
		fmt.Println("Lead no encontrado o error:", result.Error)
		return
	}

	fmt.Println("Borrando conversaciones para el lead:", lead.ID)
	// Delete physically (unscoped) to ensure it's completely gone from UI
	db.Unscoped().Where("lead_id = ?", lead.ID).Delete(&Conversation{})

	fmt.Println("Borrando el lead.")
	db.Unscoped().Delete(&lead)
	
	fmt.Println("Proceso completado exitosamente.")
}
