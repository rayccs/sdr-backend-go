package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ─── Response Helpers ────────────────────────────────────────────────────────

type Response struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{Status: "success", Data: data})
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(Response{Status: "error", Message: msg})
}

// ─── Models ──────────────────────────────────────────────────────────────────

type TestUser struct {
	gorm.Model
	Name  string
	Email string
}

type User struct {
	gorm.Model
	Email     string `gorm:"uniqueIndex;not null"`
	Name      string
	Password  string
	Provider  string
	LastLogin int64
}

// CompanyConfig almacena el "Cerebro de Ventas" de cada cliente
type CompanyConfig struct {
	gorm.Model
	CompanyID  string `gorm:"uniqueIndex"`
	Name       string
	ICP        string
	ValueOffer string
	Prompt     string
}

// Lead representa un prospecto que escribe al WhatsApp
type Lead struct {
	gorm.Model
	CompanyID    string `json:"company_id"`
	Phone        string `json:"phone"`
	Name         string `json:"name"`
	Company      string `json:"company"`
	Role         string `json:"role"`
	Status       string `json:"status"` // EN_CALIFICACION | POR_AGENDAR | HANDOFF | DESCALIFICADO | CERRADO | EN_SEGUIMIENTO
	BantScore    int    `json:"bant_score"`
	BantData     string `json:"bant_data"`   // JSON con Budget, Authority, Need, Timeline
	Pain         string `json:"pain"`        // Dolor detectado por IA
	Source       string `json:"source"`      // WHATSAPP | EMAIL | etc
	AssignedKam  string `json:"assigned_kam"` // Nombre del KAM asignado
	EnrichedData string `json:"enriched_data"`
}

// Conversation guarda el historial del chat para la IA
type Conversation struct {
	gorm.Model
	LeadID    uint   `json:"lead_id"`
	CompanyID string `json:"company_id"`
	Role      string `json:"role"`    // "user" | "assistant"
	Content   string `json:"content"`
}

// KAM representa un ejecutivo de ventas del cliente
type KAM struct {
	gorm.Model
	CompanyID string `json:"company_id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Phone     string `json:"phone"` // número WhatsApp con código de país
	Active    bool   `json:"active"`
}

var DB *gorm.DB

// ─── CORS ────────────────────────────────────────────────────────────────────

func enableCors(w *http.ResponseWriter, r *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Company-ID")
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w, r)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func getCompanyID(r *http.Request) string {
	cid := r.Header.Get("X-Company-ID")
	if cid == "" {
		cid = r.URL.Query().Get("company_id")
	}
	if cid == "" {
		cid = "default_company"
	}
	return cid
}

// sendWhatsAppMessage manda un mensaje via Evolution API
func sendWhatsAppMessage(phone, message string) error {
	evolutionURL := os.Getenv("EVOLUTION_API_URL")
	evolutionKey := os.Getenv("EVOLUTION_API_KEY")
	instanceName := os.Getenv("EVOLUTION_INSTANCE_NAME")
	if evolutionURL == "" || evolutionKey == "" || instanceName == "" {
		log.Println("WARN: Evolution API no configurada, omitiendo envío de WhatsApp")
		return nil
	}

	// Normalizar teléfono: quitar + y espacios
	cleanPhone := strings.ReplaceAll(strings.ReplaceAll(phone, "+", ""), " ", "")

	payload := map[string]interface{}{
		"number":  cleanPhone + "@s.whatsapp.net",
		"options": map[string]interface{}{"delay": 1200},
		"textMessage": map[string]interface{}{
			"text": message,
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/message/sendText/%s", evolutionURL, instanceName),
		bytes.NewBuffer(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", evolutionKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	log.Printf("Evolution API response: %d", resp.StatusCode)
	return nil
}

// ─── Main ────────────────────────────────────────────────────────────────────

func main() {
	initDB()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// ── Health ──────────────────────────────────────────────────────────────
	mux.HandleFunc("/api/health", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]string{
			"service": "SDR Backend Go",
			"status":  "operando correctamente 🚀",
			"version": "2.0.0",
		})
	}))

	mux.HandleFunc("/api/db-test", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		newUser := TestUser{Name: "Test", Email: "test@ingenylabs.com"}
		DB.Create(&newUser)
		var count int64
		DB.Model(&TestUser{}).Count(&count)
		jsonOK(w, map[string]interface{}{"total_test_users": count})
	}))

	// ── Auth (Turnstile) ─────────────────────────────────────────────────────
	verifyTurnstile := func(token string) bool {
		secret := os.Getenv("CLOUDFLARE_TURNSTILE_SECRET_KEY")
		if secret == "" {
			return true
		}
		data := url.Values{}
		data.Set("secret", secret)
		data.Set("response", token)
		resp, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", data)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var res struct {
			Success bool `json:"success"`
		}
		json.NewDecoder(resp.Body).Decode(&res)
		return res.Success
	}

	mux.HandleFunc("/api/users/login", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
			return
		}
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Name     string `json:"name"`
			Provider string `json:"provider"`
			CfToken  string `json:"cf_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "payload inválido")
			return
		}
		if req.Email == "" {
			jsonErr(w, http.StatusBadRequest, "email requerido")
			return
		}
		if req.Provider != "Google" && !verifyTurnstile(req.CfToken) {
			jsonErr(w, http.StatusForbidden, "validación de seguridad fallida")
			return
		}
		var user User
		result := DB.Where("email = ?", req.Email).First(&user)
		now := time.Now().Unix()
		if result.Error != nil {
			newUser := User{Email: req.Email, Name: req.Name, Provider: req.Provider, LastLogin: now}
			if req.Provider != "Google" && req.Password != "" {
				hashed, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
				newUser.Password = string(hashed)
			}
			DB.Create(&newUser)
			user = newUser
		} else {
			if req.Provider != "Google" {
				if user.Provider == "Google" {
					jsonErr(w, http.StatusBadRequest, "esta cuenta usa Google Sign-In")
					return
				}
				if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
					jsonErr(w, http.StatusUnauthorized, "contraseña incorrecta")
					return
				}
			}
			DB.Model(&user).Updates(User{LastLogin: now, Name: req.Name})
		}
		jsonOK(w, user)
	}))

	// ── Company Config ───────────────────────────────────────────────────────
	mux.HandleFunc("/api/company-config", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		companyID := getCompanyID(r)

		if r.Method == "GET" {
			var config CompanyConfig
			result := DB.Where("company_id = ?", companyID).First(&config)
			if result.Error != nil {
				jsonErr(w, http.StatusNotFound, "configuración no encontrada")
				return
			}
			jsonOK(w, config)
			return
		}

		if r.Method == "POST" {
			var req struct {
				CompanyID  string `json:"company_id"`
				Name       string `json:"name"`
				ICP        string `json:"icp"`
				ValueOffer string `json:"value_offer"`
				Prompt     string `json:"prompt"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonErr(w, http.StatusBadRequest, "payload inválido")
				return
			}
			if req.CompanyID != "" {
				companyID = req.CompanyID
			}

			var config CompanyConfig
			result := DB.Where("company_id = ?", companyID).First(&config)
			if result.Error != nil {
				config = CompanyConfig{CompanyID: companyID, Name: req.Name, ICP: req.ICP, ValueOffer: req.ValueOffer, Prompt: req.Prompt}
				DB.Create(&config)
			} else {
				DB.Model(&config).Updates(CompanyConfig{Name: req.Name, ICP: req.ICP, ValueOffer: req.ValueOffer, Prompt: req.Prompt})
			}
			jsonOK(w, config)
			return
		}

		jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
	}))

	// ── Leads ────────────────────────────────────────────────────────────────
	mux.HandleFunc("/api/leads", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		companyID := getCompanyID(r)

		switch r.Method {
		case "GET":
			// Filtros opcionales
			statusFilter := r.URL.Query().Get("status")
			var leads []Lead
			query := DB.Where("company_id = ?", companyID).Order("created_at DESC")
			if statusFilter != "" && statusFilter != "TODOS" {
				query = query.Where("status = ?", statusFilter)
			}
			query.Find(&leads)
			jsonOK(w, leads)

		case "POST":
			var lead Lead
			if err := json.NewDecoder(r.Body).Decode(&lead); err != nil {
				jsonErr(w, http.StatusBadRequest, "payload inválido")
				return
			}
			lead.CompanyID = companyID
			if lead.Status == "" {
				lead.Status = "EN_CALIFICACION"
			}
			if lead.Source == "" {
				lead.Source = "WHATSAPP"
			}

			// Si ya existe un lead con ese teléfono y empresa, actualizarlo
			var existing Lead
			result := DB.Where("company_id = ? AND phone = ?", companyID, lead.Phone).First(&existing)
			if result.Error == nil {
				DB.Model(&existing).Updates(map[string]interface{}{
					"bant_score": lead.BantScore,
					"bant_data":  lead.BantData,
					"pain":       lead.Pain,
					"status":     lead.Status,
				})
				jsonOK(w, existing)
				return
			}

			DB.Create(&lead)
			jsonOK(w, lead)

		default:
			jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
		}
	}))

	// Endpoint para un lead específico (por ID)
	mux.HandleFunc("/api/leads/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// Extraer ID del path: /api/leads/123
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/leads/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			jsonErr(w, http.StatusBadRequest, "ID requerido")
			return
		}
		leadID, err := strconv.Atoi(parts[0])
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "ID inválido")
			return
		}

		companyID := getCompanyID(r)

		switch r.Method {
		case "GET":
			var lead Lead
			if err := DB.Where("id = ? AND company_id = ?", leadID, companyID).First(&lead).Error; err != nil {
				jsonErr(w, http.StatusNotFound, "lead no encontrado")
				return
			}
			jsonOK(w, lead)

		case "PUT":
			var updates map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
				jsonErr(w, http.StatusBadRequest, "payload inválido")
				return
			}
			var lead Lead
			if err := DB.Where("id = ? AND company_id = ?", leadID, companyID).First(&lead).Error; err != nil {
				jsonErr(w, http.StatusNotFound, "lead no encontrado")
				return
			}
			DB.Model(&lead).Updates(updates)
			jsonOK(w, lead)

		default:
			jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
		}
	}))

	// ── Conversations ────────────────────────────────────────────────────────
	mux.HandleFunc("/api/conversations", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		companyID := getCompanyID(r)

		switch r.Method {
		case "GET":
			leadIDStr := r.URL.Query().Get("lead_id")
			if leadIDStr == "" {
				jsonErr(w, http.StatusBadRequest, "lead_id requerido")
				return
			}
			leadID, err := strconv.Atoi(leadIDStr)
			if err != nil {
				jsonErr(w, http.StatusBadRequest, "lead_id inválido")
				return
			}
			var convs []Conversation
			DB.Where("lead_id = ? AND company_id = ?", leadID, companyID).Order("created_at ASC").Find(&convs)
			jsonOK(w, convs)

		case "POST":
			var conv Conversation
			if err := json.NewDecoder(r.Body).Decode(&conv); err != nil {
				jsonErr(w, http.StatusBadRequest, "payload inválido")
				return
			}
			conv.CompanyID = companyID
			DB.Create(&conv)
			jsonOK(w, conv)

		default:
			jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
		}
	}))

	// ── KAMs ────────────────────────────────────────────────────────────────
	mux.HandleFunc("/api/kams", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		companyID := getCompanyID(r)

		switch r.Method {
		case "GET":
			var kams []KAM
			DB.Where("company_id = ? AND active = true", companyID).Find(&kams)
			jsonOK(w, kams)

		case "POST":
			var kam KAM
			if err := json.NewDecoder(r.Body).Decode(&kam); err != nil {
				jsonErr(w, http.StatusBadRequest, "payload inválido")
				return
			}
			kam.CompanyID = companyID
			kam.Active = true
			DB.Create(&kam)
			jsonOK(w, kam)

		default:
			jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
		}
	}))

	// ── Handoff ──────────────────────────────────────────────────────────────
	// Asigna un KAM a un lead y le envía un mensaje de WhatsApp con el brief
	mux.HandleFunc("/api/handoff", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
			return
		}

		var req struct {
			LeadID  uint   `json:"lead_id"`
			KamName string `json:"kam_name"`
			KamPhone string `json:"kam_phone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "payload inválido")
			return
		}
		if req.LeadID == 0 || req.KamPhone == "" {
			jsonErr(w, http.StatusBadRequest, "lead_id y kam_phone son requeridos")
			return
		}

		companyID := getCompanyID(r)

		// Obtener datos del lead
		var lead Lead
		if err := DB.Where("id = ? AND company_id = ?", req.LeadID, companyID).First(&lead).Error; err != nil {
			jsonErr(w, http.StatusNotFound, "lead no encontrado")
			return
		}

		// Actualizar lead con el KAM asignado
		DB.Model(&lead).Updates(map[string]interface{}{
			"status":       "HANDOFF",
			"assigned_kam": req.KamName,
		})

		// Obtener las últimas conversaciones para el brief
		var convs []Conversation
		DB.Where("lead_id = ? AND company_id = ?", lead.ID, companyID).
			Order("created_at DESC").Limit(6).Find(&convs)

		// Construir el mensaje de handoff para el KAM
		briefLines := []string{
			fmt.Sprintf("🤖 *SDR Cognitivo — Nuevo Lead Listo para Cierre*"),
			fmt.Sprintf(""),
			fmt.Sprintf("👤 *Lead:* %s", lead.Name),
			fmt.Sprintf("🏢 *Empresa:* %s", lead.Company),
			fmt.Sprintf("💼 *Rol:* %s", lead.Role),
			fmt.Sprintf("📱 *Contacto:* %s", lead.Phone),
			fmt.Sprintf("🎯 *Score BANT:* %d/100", lead.BantScore),
			fmt.Sprintf("😣 *Dolor detectado:* %s", lead.Pain),
			fmt.Sprintf(""),
			fmt.Sprintf("💡 *Acción recomendada:* Contactar lo antes posible. El prospecto tiene alta intención de compra."),
			fmt.Sprintf(""),
			fmt.Sprintf("🔗 Ver en plataforma: https://os.ingenylabs.com/marketing/sdr"),
		}
		handoffMsg := strings.Join(briefLines, "\n")

		// Enviar mensaje al KAM
		if err := sendWhatsAppMessage(req.KamPhone, handoffMsg); err != nil {
			log.Printf("Error enviando handoff a KAM: %v", err)
			// No falla el endpoint aunque el WhatsApp falle
		}

		jsonOK(w, map[string]interface{}{
			"lead":    lead,
			"message": "Handoff realizado y KAM notificado",
		})
	}))

	// ── Webhook de N8N: recibe mensaje + score y crea/actualiza lead ─────────
	// N8N llama a este endpoint después de que el Brain procesa el mensaje
	mux.HandleFunc("/api/webhook/message", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
			return
		}

		var req struct {
			Phone     string `json:"phone"`
			Text      string `json:"text"`    // Mensaje del prospecto
			Response  string `json:"response"` // Respuesta de la IA
			BantScore int    `json:"bant_score"`
			Status    string `json:"status"`
			Pain      string `json:"pain"`
			CompanyID string `json:"company_id"`
			Name      string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "payload inválido")
			return
		}

		companyID := req.CompanyID
		if companyID == "" {
			companyID = "default_company"
		}

		// Buscar o crear el lead
		var lead Lead
		result := DB.Where("company_id = ? AND phone = ?", companyID, req.Phone).First(&lead)
		if result.Error != nil {
			// Crear nuevo lead
			lead = Lead{
				CompanyID: companyID,
				Phone:     req.Phone,
				Name:      req.Name,
				Source:    "WHATSAPP",
				Status:    "EN_CALIFICACION",
				BantScore: req.BantScore,
				Pain:      req.Pain,
			}
			DB.Create(&lead)
		} else {
			// Actualizar score y status
			updates := map[string]interface{}{"bant_score": req.BantScore}
			if req.Status != "" {
				updates["status"] = req.Status
			}
			if req.Pain != "" {
				updates["pain"] = req.Pain
			}
			DB.Model(&lead).Updates(updates)
		}

		// Guardar mensajes en historial de conversación
		if req.Text != "" {
			DB.Create(&Conversation{
				LeadID:    lead.ID,
				CompanyID: companyID,
				Role:      "user",
				Content:   req.Text,
			})
		}
		if req.Response != "" {
			DB.Create(&Conversation{
				LeadID:    lead.ID,
				CompanyID: companyID,
				Role:      "assistant",
				Content:   req.Response,
			})
		}

		jsonOK(w, map[string]interface{}{
			"lead_id":    lead.ID,
			"bant_score": lead.BantScore,
			"status":     lead.Status,
		})
	}))

	// ── WhatsApp Evolution API ────────────────────────────────────────────────
	mux.HandleFunc("/api/whatsapp/connect", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			jsonErr(w, http.StatusMethodNotAllowed, "método no permitido")
			return
		}
		companyID := getCompanyID(r)
		instanceName := "sdr-" + companyID

		evolutionURL := os.Getenv("EVOLUTION_API_URL")
		evolutionKey := os.Getenv("EVOLUTION_API_KEY")

		if evolutionURL == "" || evolutionKey == "" {
			jsonErr(w, http.StatusInternalServerError, "Evolution API no configurada en el backend")
			return
		}

		// Intentar crear instancia
		payload := map[string]interface{}{
			"instanceName": instanceName,
			"token":        instanceName,
			"qrcode":       true,
			"integration":  "WHATSAPP-BAILEYS",
		}
		body, _ := json.Marshal(payload)
		
		req, _ := http.NewRequest("POST", evolutionURL+"/instance/create", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("apikey", evolutionKey)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Error conectando a Evolution API")
			return
		}
		defer resp.Body.Close()

		// Si ya existe (403 o error), intentamos pedir el connect
		if resp.StatusCode != 201 && resp.StatusCode != 200 {
			req, _ = http.NewRequest("GET", evolutionURL+"/instance/connect/"+instanceName, nil)
			req.Header.Set("apikey", evolutionKey)
			resp2, err2 := client.Do(req)
			if err2 != nil {
				jsonErr(w, http.StatusInternalServerError, "Error obteniendo QR")
				return
			}
			defer resp2.Body.Close()
			var data interface{}
			json.NewDecoder(resp2.Body).Decode(&data)
			jsonOK(w, data)
			return
		}

		var data interface{}
		json.NewDecoder(resp.Body).Decode(&data)
		jsonOK(w, data)
	}))

	mux.HandleFunc("/api/whatsapp/status", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		companyID := getCompanyID(r)
		instanceName := "sdr-" + companyID
		evolutionURL := os.Getenv("EVOLUTION_API_URL")
		evolutionKey := os.Getenv("EVOLUTION_API_KEY")

		req, _ := http.NewRequest("GET", evolutionURL+"/instance/connectionState/"+instanceName, nil)
		req.Header.Set("apikey", evolutionKey)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Error conectando a Evolution API")
			return
		}
		defer resp.Body.Close()
		
		var data interface{}
		json.NewDecoder(resp.Body).Decode(&data)
		jsonOK(w, data)
	}))

	fmt.Printf("✅ SDR Backend Go v2.0 iniciado en puerto %s\n", port)
	if err := http.ListenAndServe("0.0.0.0:"+port, mux); err != nil {
		log.Fatalf("Error al arrancar el servidor: %v", err)
	}
}

// ─── DB Init ─────────────────────────────────────────────────────────────────

func initDB() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=127.0.0.1 user=postgres password=admin dbname=postgres port=5432 sslmode=disable TimeZone=UTC"
	}

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("❌ Error conectando a PostgreSQL: %v", err)
	}

	fmt.Println("✅ Conectado a PostgreSQL exitosamente.")

	err = DB.AutoMigrate(
		&TestUser{},
		&User{},
		&CompanyConfig{},
		&Lead{},
		&Conversation{},
		&KAM{},
	)
	if err != nil {
		log.Printf("⚠️ Error migrando BD: %v", err)
	} else {
		fmt.Println("✅ Tablas sincronizadas (v2).")
	}
}
