package main

import (
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type TestUser struct { gorm.Model }
type User struct { gorm.Model }
type CompanyConfig struct { gorm.Model }
type Lead struct { gorm.Model }
type Conversation struct { gorm.Model }
type KAM struct { gorm.Model }

func main() {
	fmt.Println("🚀 Iniciando WIPE TOTAL de la plataforma (Base de Datos)...")

	dsn := "postgres://postgres.ulpttibobjmzctrgiiey:SdrWhastapp2026Base@aws-0-ca-central-1.pooler.supabase.com:5432/postgres"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("❌ Error conectando a PostgreSQL: %v", err)
	}

	fmt.Println("✅ Conectado a PostgreSQL.")
	
	// Limpieza total (en orden para evitar problemas de dependencias si las hubiera)
	tables := []struct {
		model interface{}
		name  string
	}{
		{&Conversation{}, "conversations"},
		{&Lead{}, "leads"},
		{&KAM{}, "k_a_ms"},
		{&CompanyConfig{}, "company_configs"},
		{&User{}, "users"},
		{&TestUser{}, "test_users"},
	}

	for _, t := range tables {
		result := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(t.model)
		fmt.Printf("🗑️ Se eliminaron %d registros de %s.\n", result.RowsAffected, t.name)
	}

	fmt.Println("✨ Limpieza de Base de Datos completada con éxito.")
}
