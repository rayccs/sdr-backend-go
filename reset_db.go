//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func loadEnvVar(key, defaultVal string) string {
	file, err := os.Open(".env")
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			prefix := key + "="
			if strings.HasPrefix(line, prefix) {
				val := strings.TrimPrefix(line, prefix)
				return strings.Trim(val, "\"'\n\r ")
			}
		}
	}
	return defaultVal
}

func main() {
	fmt.Println("==================================================")
	fmt.Println("🚀 INICIANDO REINICIO COMPLETO DE PLATAFORMA (0) ")
	fmt.Println("==================================================")

	// 1. Limpiar PostgreSQL (Supabase)
	dsn := loadEnvVar("DATABASE_URL", "postgres://postgres.ulpttibobjmzctrgiiey:SdrWhastapp2026Base@aws-0-ca-central-1.pooler.supabase.com:5432/postgres")
	fmt.Println("\n[1/4] Conectando a PostgreSQL (Supabase)...")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Printf("⚠️ Error conectando a PostgreSQL: %v", err)
	} else {
		tables := []string{
			"users",
			"kams",
			"company_configs",
			"leads",
			"conversations",
			"raw_logs",
			"file_stores",
			"test_users",
		}
		for _, table := range tables {
			errTrunc := db.Exec(fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE;", table)).Error
			if errTrunc != nil {
				db.Exec(fmt.Sprintf("DELETE FROM %s;", table))
				fmt.Printf("   ✅ Tabla %-16s vaciada (DELETE)\n", table)
			} else {
				fmt.Printf("   ✅ Tabla %-16s vaciada (TRUNCATE)\n", table)
			}
		}
	}

	// 2. Desvincular y eliminar todas las instancias de WhatsApp en Evolution API
	evolutionURL := loadEnvVar("EVOLUTION_API_URL", "https://evolution-api-latest-ursr.onrender.com")
	evolutionKey := loadEnvVar("EVOLUTION_API_KEY", "ingeny-api-key-secreta")
	fmt.Println("\n[2/4] Desvinculando números de WhatsApp en Evolution API...")
	client := &http.Client{Timeout: 15 * time.Second}
	reqInst, _ := http.NewRequest("GET", evolutionURL+"/instance/fetchInstances", nil)
	reqInst.Header.Set("apikey", evolutionKey)
	respInst, errInst := client.Do(reqInst)
	if errInst == nil {
		defer respInst.Body.Close()
		var instances []struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(respInst.Body).Decode(&instances) == nil {
			for _, inst := range instances {
				// Logout
				reqLo, _ := http.NewRequest("DELETE", evolutionURL+"/instance/logout/"+inst.Name, nil)
				reqLo.Header.Set("apikey", evolutionKey)
				client.Do(reqLo)
				// Delete
				reqDel, _ := http.NewRequest("DELETE", evolutionURL+"/instance/delete/"+inst.Name, nil)
				reqDel.Header.Set("apikey", evolutionKey)
				client.Do(reqDel)
				fmt.Printf("   ✅ Instancia WhatsApp desvinculada y eliminada: %s\n", inst.Name)
			}
			if len(instances) == 0 {
				fmt.Println("   ✅ No hay instancias de WhatsApp pendientes.")
			}
		}
	} else {
		fmt.Printf("   ⚠️ No se pudo conectar a Evolution API: %v\n", errInst)
	}

	// 3. Limpiar Redis en Upstash (Caché y Rate Limits)
	redisToken := "gQAAAAAABGcGAAIgcDJiMjYxZTBlMmZmZjA0YmJmOThiMTcyODJjNDg5ZmJkOQ"
	redisURL := "https://primary-redfish-288518.upstash.io/flushdb"
	fmt.Println("\n[3/4] Vaciando Upstash Redis (Rate-limits y semántica)...")
	reqRedis, _ := http.NewRequest("POST", redisURL, nil)
	reqRedis.Header.Set("Authorization", "Bearer "+redisToken)
	respRedis, errRedis := client.Do(reqRedis)
	if errRedis == nil {
		defer respRedis.Body.Close()
		body, _ := io.ReadAll(respRedis.Body)
		fmt.Printf("   ✅ Redis vaciado (FLUSHDB): %s\n", string(body))
	} else {
		fmt.Printf("   ⚠️ Error vaciando Redis: %v\n", errRedis)
	}

	// 4. Limpiar SQLite local si existiera
	fmt.Println("\n[4/4] Limpiando archivos locales de desarrollo...")
	sqlitePath := "sdr_backend.db"
	if _, errStat := os.Stat(sqlitePath); errStat == nil {
		os.Remove(sqlitePath)
		fmt.Println("   ✅ Base local sdr_backend.db eliminada")
	} else {
		fmt.Println("   ✅ No hay bases SQLite locales temporales.")
	}

	fmt.Println("\n==================================================")
	fmt.Println("🎉 ¡TODO EL SISTEMA HA SIDO REINICIADO A CERO (0)!")
	fmt.Println("==================================================")
}
