package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Response representa la estructura base de nuestras respuestas JSON
type Response struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// TestUser es un modelo de prueba para la base de datos
type TestUser struct {
	gorm.Model
	Name  string
	Email string
}

// User representa a los usuarios del sistema que inician sesión
type User struct {
	gorm.Model
	Email     string `gorm:"uniqueIndex;not null"`
	Name      string
	Password  string // Hasheada (vacía si es Google)
	Provider  string // "Google" o "Email"
	LastLogin int64  // Unix timestamp
}

var DB *gorm.DB

func main() {
	// 1. Inicializar la base de datos
	initDB()

	// 2. Definimos un puerto por defecto
	port := "8080"
	mux := http.NewServeMux()

	// Endpoint original de Health Check
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w, r)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: "Ingeny OS Backend operando correctamente 🚀",
		})
	})

	// Nuevo Endpoint para probar la BD
	mux.HandleFunc("/api/db-test", func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w, r)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		// Creamos un registro de prueba
		newUser := TestUser{Name: "Usuario de Prueba", Email: "test@ingenylabs.com"}
		result := DB.Create(&newUser)
		
		if result.Error != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{
				Status:  "error",
				Message: "No se pudo crear el usuario en la BD",
				Data:    result.Error.Error(),
			})
			return
		}

		// Buscamos cuántos usuarios hay en total para confirmar que guardó
		var count int64
		DB.Model(&TestUser{}).Count(&count)

		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: "¡Conexión a PostgreSQL y escritura exitosa! 🎉",
			Data: map[string]interface{}{
				"created_user": newUser,
				"total_users":  count,
			},
		})
	})

	// Helper para verificar Turnstile
	verifyTurnstile := func(token string) bool {
		secret := os.Getenv("CLOUDFLARE_TURNSTILE_SECRET_KEY")
		if secret == "" {
			// Fallback o advertencia en modo desarrollo si no está la llave
			log.Println("WARNING: CLOUDFLARE_TURNSTILE_SECRET_KEY no está configurada")
			return true // Permitimos para evitar bloqueo local si no la configuran
		}
		
		data := url.Values{}
		data.Set("secret", secret)
		data.Set("response", token)

		resp, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", data)
		if err != nil {
			log.Printf("Error llamando a Turnstile: %v", err)
			return false
		}
		defer resp.Body.Close()

		var resData struct {
			Success bool `json:"success"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&resData); err != nil {
			return false
		}
		return resData.Success
	}

	// Endpoint para registrar el inicio de sesión desde el frontend
	mux.HandleFunc("/api/users/login", func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w, r)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var reqData struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Name     string `json:"name"`
			Provider string `json:"provider"`
			CfToken  string `json:"cf_token"`
		}

		if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(Response{Status: "error", Message: "Payload inválido"})
			return
		}

		if reqData.Email == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(Response{Status: "error", Message: "Email es requerido"})
			return
		}

		// Si es un inicio de sesión por Email, validar Turnstile
		if reqData.Provider != "Google" {
			if !verifyTurnstile(reqData.CfToken) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(Response{Status: "error", Message: "Validación de seguridad fallida (Bot detectado)"})
				return
			}
		}

		var user User
		result := DB.Where("email = ?", reqData.Email).First(&user)
		currentTime := time.Now().Unix()

		if result.Error != nil {
			// El usuario no existe, crearlo
			newUser := User{
				Email:     reqData.Email,
				Name:      reqData.Name,
				Provider:  reqData.Provider,
				LastLogin: currentTime,
			}
			
			if reqData.Provider != "Google" && reqData.Password != "" {
				hashedPassword, err := bcrypt.GenerateFromPassword([]byte(reqData.Password), bcrypt.DefaultCost)
				if err == nil {
					newUser.Password = string(hashedPassword)
				}
			}
			
			DB.Create(&newUser)
			user = newUser
		} else {
			// El usuario ya existe. 
			// Si el proveedor es "Email", validar la contraseña
			if reqData.Provider != "Google" {
				if user.Provider == "Google" {
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(Response{Status: "error", Message: "Esta cuenta está vinculada a Google. Usa Google para iniciar sesión."})
					return
				}
				
				err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(reqData.Password))
				if err != nil {
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(Response{Status: "error", Message: "Contraseña incorrecta"})
					return
				}
			}

			DB.Model(&user).Updates(User{
				LastLogin: currentTime,
				Name:      reqData.Name,
				Provider:  reqData.Provider,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: "Usuario autenticado exitosamente",
			Data:    user,
		})
	})

	fmt.Printf("Servidor Backend de Go iniciado en http://127.0.0.1:%s\n", port)
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		log.Fatalf("Error al arrancar el servidor: %v", err)
	}
}

func initDB() {
	// Connection string apuntando al contenedor local
	dsn := "host=127.0.0.1 user=postgres password=admin dbname=postgres port=5432 sslmode=disable TimeZone=UTC"
	
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("❌ Error conectando a PostgreSQL: %v\n(Asegúrate de que el contenedor Docker esté corriendo)", err)
	}

	fmt.Println("✅ Conectado a PostgreSQL exitosamente.")

	// Auto-Migrar las estructuras a PostgreSQL
	err = DB.AutoMigrate(&TestUser{}, &User{})
	if err != nil {
		log.Printf("⚠️ Error migrando base de datos: %v", err)
	} else {
		fmt.Println("✅ Tablas sincronizadas exitosamente.")
	}
}

func enableCors(w *http.ResponseWriter, r *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}
