package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

type LoginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

type Claims struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type App struct {
	db              *sql.DB
	jwtSecret       []byte
	allowedOrigins  map[string]bool
	revokedSessions map[string]time.Time
	revokedMu       sync.Mutex
}

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Could not load .env: %v", err)
	}

	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		log.Fatal("JWT_SECRET must contain at least 32 characters")
	}

	port := envOrDefault("PORT", "8080")
	dsn := mysql.Config{
		User:   envOrDefault("MYSQL_USER", "root"),
		Passwd: os.Getenv("MYSQL_PASSWORD"),
		// Config literals do not inherit the driver's DSN defaults. XAMPP
		// MariaDB authenticates this account with mysql_native_password.
		AllowNativePasswords: true,
		Net:                  "tcp",
		Addr:                 net.JoinHostPort(envOrDefault("MYSQL_HOST", "127.0.0.1"), envOrDefault("MYSQL_PORT", "3306")),
		DBName:               envOrDefault("MYSQL_DATABASE", "helpdesk_db"),
		ParseTime:            true,
		Params:               map[string]string{"charset": "utf8mb4"},
	}

	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		log.Fatalf("Could not configure MySQL: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("Could not connect to XAMPP MariaDB: %v", err)
	}

	origins := make(map[string]bool)
	for _, origin := range strings.Split(envOrDefault("CORS_ORIGINS", "http://localhost:4200"), ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = true
		}
	}

	app := &App{
		db:              db,
		jwtSecret:       []byte(secret),
		allowedOrigins:  origins,
		revokedSessions: make(map[string]time.Time),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", app.login)
	mux.HandleFunc("GET /api/auth/me", app.me)
	mux.HandleFunc("POST /api/auth/logout", app.logout)
	mux.HandleFunc("GET /api/categories", app.listCategories)
	mux.HandleFunc("GET /api/tickets", app.listTickets)
	mux.HandleFunc("GET /api/tickets/{ticketNumber}", app.getTicket)
	mux.HandleFunc("GET /api/tickets/{ticketNumber}/comments", app.listTicketComments)
	mux.HandleFunc("POST /api/tickets/{ticketNumber}/comments", app.addTicketComment)
	mux.HandleFunc("POST /api/tickets", app.createTicket)
	mux.HandleFunc("PATCH /api/tickets/{ticketNumber}/status", app.updateTicketStatus)
	mux.HandleFunc("PATCH /api/tickets/{ticketNumber}/closure", app.reviewTicketClosure)
	mux.HandleFunc("PATCH /api/tickets/{ticketNumber}/resolution-review", app.reviewTicketResolution)
	mux.HandleFunc("PATCH /api/tickets/{ticketNumber}/resolution-confirmation", app.confirmTicketResolution)
	mux.HandleFunc("PATCH /api/tickets/{ticketNumber}/assignment", app.assignTicket)
	mux.HandleFunc("PATCH /api/tickets/{ticketNumber}/priority", app.updateTicketPriority)
	mux.HandleFunc("PATCH /api/tickets/{ticketNumber}", app.updateTicket)
	mux.HandleFunc("GET /api/support-users", app.listSupportUsers)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           app.cors(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Connected to MariaDB database %q", dsn.DBName)
	log.Printf("HelpDesk API listening on http://localhost:%s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type Category struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CreateTicketRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	CategoryID  int64  `json:"category_id"`
	Priority    string `json:"priority"`
}

type TicketResponse struct {
	ID                    int64     `json:"id"`
	TicketNumber          string    `json:"ticket_number"`
	Title                 string    `json:"title"`
	Description           string    `json:"description"`
	CategoryID            int64     `json:"category_id"`
	Priority              string    `json:"priority"`
	Status                string    `json:"status"`
	AssignedTo            *int64    `json:"assigned_to"`
	CreatedBy             int64     `json:"created_by"`
	Creator               User      `json:"creator"`
	ResolutionDescription *string   `json:"resolution_description"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type TicketListItem struct {
	ID                    int64     `json:"id"`
	TicketNumber          string    `json:"ticket_number"`
	Title                 string    `json:"title"`
	Description           string    `json:"description"`
	CategoryID            int64     `json:"category_id"`
	CategoryName          string    `json:"category_name"`
	Priority              string    `json:"priority"`
	Status                string    `json:"status"`
	AssignedTo            *int64    `json:"assigned_to"`
	AssignedToName        string    `json:"assigned_to_name"`
	CreatedBy             int64     `json:"created_by"`
	CreatedByName         string    `json:"created_by_name"`
	ResolutionDescription *string   `json:"resolution_description"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

const ticketSelect = `
	SELECT t.id, t.ticket_number, t.title, t.description, t.category_id, c.name,
	       t.priority, t.status, t.assigned_to, COALESCE(assigned.full_name, ''),
	       t.created_by, creator.full_name, t.resolution_description, t.created_at, t.updated_at
	FROM tickets t
	JOIN categories c ON c.id = t.category_id
	JOIN users creator ON creator.id = t.created_by
	LEFT JOIN users assigned ON assigned.id = t.assigned_to
`

func (a *App) listTickets(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	params := r.URL.Query()
	search := strings.TrimSpace(params.Get("search"))
	if len([]rune(search)) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Search must be 200 characters or fewer"})
		return
	}
	priority := strings.ToUpper(strings.TrimSpace(params.Get("priority")))
	if priority != "" && priority != "LOW" && priority != "MEDIUM" && priority != "HIGH" && priority != "CRITICAL" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ticket priority filter"})
		return
	}
	status := strings.ToUpper(strings.TrimSpace(params.Get("status")))
	validStatuses := map[string]bool{
		"OPEN": true, "ASSIGNED": true, "IN_PROGRESS": true, "RESOLVED": true,
		"PENDING_APPROVAL": true, "PENDING_RESOLUTION_APPROVAL": true,
		"PENDING_USER_CONFIRMATION": true, "CLOSED": true,
	}
	if status != "" && !validStatuses[status] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ticket status filter"})
		return
	}

	conditions := make([]string, 0, 4)
	args := make([]any, 0, 6)
	switch claims.Role {
	case "ADMIN":
	case "SUPPORT":
		conditions = append(conditions, `t.assigned_to = ?`)
		args = append(args, claims.UserID)
	case "USER":
		conditions = append(conditions, `t.created_by = ?`)
		args = append(args, claims.UserID)
	default:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Your role cannot view tickets"})
		return
	}
	if search != "" {
		conditions = append(conditions, `(t.ticket_number LIKE ? OR t.title LIKE ?)`)
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern)
	}
	if priority != "" {
		conditions = append(conditions, `t.priority = ?`)
		args = append(args, priority)
	}
	if status != "" {
		conditions = append(conditions, `t.status = ?`)
		args = append(args, status)
	}
	where := ""
	if len(conditions) > 0 {
		where = ` WHERE ` + strings.Join(conditions, ` AND `)
	}

	var total int64
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM tickets t`+where, args...).Scan(&total); err != nil {
		log.Printf("Ticket count query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not count tickets"})
		return
	}

	// Pagination is opt-in so existing callers (including the dashboard) keep
	// receiving the complete role-scoped list unless they request a page.
	page, limit := 1, int(total)
	paginated := params.Has("page") || params.Has("limit")
	if paginated {
		page, limit = 1, 20
		if raw := params.Get("page"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 1_000_000_000 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Page must be a positive integer"})
				return
			}
			page = value
		}
		if raw := params.Get("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Limit must be between 1 and 100"})
				return
			}
			limit = value
		}
	} else if limit < 1 {
		limit = 1
	}

	query := ticketSelect + where + ` ORDER BY t.created_at DESC, t.id DESC`
	queryArgs := append([]any(nil), args...)
	if paginated {
		query += ` LIMIT ? OFFSET ?`
		queryArgs = append(queryArgs, limit, (page-1)*limit)
	}
	tickets, err := a.queryTickets(r, query, queryArgs...)
	if err != nil {
		log.Printf("Ticket list query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load tickets"})
		return
	}
	response := map[string]any{"tickets": tickets}
	if paginated {
		pages := (total + int64(limit) - 1) / int64(limit)
		response["pagination"] = map[string]int64{
			"page": int64(page), "limit": int64(limit), "total": total, "total_pages": pages,
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) getTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	query := ticketSelect + ` WHERE t.ticket_number = ?`
	args := []any{r.PathValue("ticketNumber")}
	switch claims.Role {
	case "ADMIN":
	case "SUPPORT":
		query += ` AND t.assigned_to = ?`
		args = append(args, claims.UserID)
	case "USER":
		query += ` AND t.created_by = ?`
		args = append(args, claims.UserID)
	default:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Your role cannot view tickets"})
		return
	}
	tickets, err := a.queryTickets(r, query+` LIMIT 1`, args...)
	if err != nil {
		log.Printf("Ticket detail query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load ticket details"})
		return
	}
	if len(tickets) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
		return
	}
	writeJSON(w, http.StatusOK, tickets[0])
}

type TicketComment struct {
	ID        int64     `json:"id"`
	TicketID  int64     `json:"ticket_id"`
	UserID    int64     `json:"user_id"`
	UserName  string    `json:"user_name"`
	Text      string    `json:"comment_text"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateCommentRequest struct {
	Text string `json:"comment_text"`
}

func (a *App) authorizedTicket(ctx context.Context, claims *Claims, number string) (int64, string, error) {
	query := `SELECT id, status FROM tickets WHERE ticket_number = ?`
	args := []any{number}
	switch claims.Role {
	case "ADMIN":
	case "SUPPORT":
		query += ` AND assigned_to = ?`
		args = append(args, claims.UserID)
	case "USER":
		query += ` AND created_by = ?`
		args = append(args, claims.UserID)
	default:
		return 0, "", errors.New("role cannot access tickets")
	}
	var id int64
	var status string
	err := a.db.QueryRowContext(ctx, query, args...).Scan(&id, &status)
	return id, status, err
}

func (a *App) listTicketComments(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	ticketID, _, err := a.authorizedTicket(r.Context(), claims, r.PathValue("ticketNumber"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Your role cannot view comments for this ticket"})
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT c.id, c.ticket_id, c.user_id, u.full_name, c.comment_text, c.created_at FROM comments c JOIN users u ON u.id = c.user_id WHERE c.ticket_id = ? ORDER BY c.created_at ASC, c.id ASC`, ticketID)
	if err != nil {
		log.Printf("Ticket comments query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load ticket comments"})
		return
	}
	defer rows.Close()
	comments := make([]TicketComment, 0)
	for rows.Next() {
		var comment TicketComment
		if err := rows.Scan(&comment.ID, &comment.TicketID, &comment.UserID, &comment.UserName, &comment.Text, &comment.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load ticket comments"})
			return
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load ticket comments"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": comments})
}

func (a *App) addTicketComment(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	ticketID, status, err := a.authorizedTicket(r.Context(), claims, r.PathValue("ticketNumber"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Your role cannot comment on this ticket"})
		return
	}
	if status == "CLOSED" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Closed tickets cannot be edited"})
		return
	}
	var req CreateCommentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid comment"})
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || len([]rune(req.Text)) > 5000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Comment is required and must be 5,000 characters or fewer"})
		return
	}
	result, err := a.db.ExecContext(r.Context(), `INSERT INTO comments (ticket_id, user_id, comment_text) VALUES (?, ?, ?)`, ticketID, claims.UserID, req.Text)
	if err != nil {
		log.Printf("Ticket comment insert error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save comment"})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not read saved comment"})
		return
	}
	var comment TicketComment
	err = a.db.QueryRowContext(r.Context(), `SELECT c.id, c.ticket_id, c.user_id, u.full_name, c.comment_text, c.created_at FROM comments c JOIN users u ON u.id = c.user_id WHERE c.id = ?`, id).Scan(&comment.ID, &comment.TicketID, &comment.UserID, &comment.UserName, &comment.Text, &comment.CreatedAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Comment was saved but could not be reloaded"})
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (a *App) listSupportUsers(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	if claims.Role != "ADMIN" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only administrators can view support assignments"})
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id, full_name, email, role FROM users WHERE role = 'SUPPORT' ORDER BY full_name`)
	if err != nil {
		log.Printf("Support user list error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load support users"})
		return
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.FullName, &user.Email, &user.Role); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load support users"})
			return
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load support users"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

type TicketStatusRequest struct {
	Status                string `json:"status"`
	ResolutionDescription string `json:"resolution_description"`
}

type TicketAssignmentRequest struct {
	AssignedTo *int64 `json:"assigned_to"`
}

type TicketUpdateRequest struct {
	Status                string `json:"status"`
	AssignedTo            *int64 `json:"assigned_to"`
	ResolutionDescription string `json:"resolution_description"`
}

type TicketPriorityRequest struct {
	Priority string `json:"priority"`
}

// updateTicket saves an administrator's status and assignee changes atomically.
func (a *App) updateTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	if claims.Role != "ADMIN" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only administrators can update ticket assignment and status together"})
		return
	}
	var req TicketUpdateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ticket update"})
		return
	}
	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))
	if req.Status != "OPEN" && req.Status != "ASSIGNED" && req.Status != "IN_PROGRESS" && req.Status != "RESOLVED" && req.Status != "CLOSED" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ticket status"})
		return
	}
	req.ResolutionDescription = strings.TrimSpace(req.ResolutionDescription)
	if req.Status == "RESOLVED" && req.ResolutionDescription == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "A resolution description is required to resolve this ticket"})
		return
	}
	if req.AssignedTo != nil {
		var exists int
		if err := a.db.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE id = ? AND role = 'SUPPORT'`, *req.AssignedTo).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Tickets can only be assigned to a support user"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not validate support user"})
			return
		}
	} else {
		var priority string
		if err := a.db.QueryRowContext(r.Context(), `SELECT priority FROM tickets WHERE ticket_number = ?`, r.PathValue("ticketNumber")).Scan(&priority); err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not validate ticket assignment"})
			return
		} else if priority == "CRITICAL" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Critical tickets must remain assigned to a support user"})
			return
		}
	}
	statusToStore := req.Status
	if req.Status == "RESOLVED" {
		statusToStore = "PENDING_USER_CONFIRMATION"
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save ticket changes"})
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE tickets SET status = ?, assigned_to = ?, resolution_description = CASE WHEN ? <> '' THEN ? ELSE resolution_description END WHERE ticket_number = ? AND status NOT IN ('CLOSED', 'PENDING_APPROVAL', 'PENDING_RESOLUTION_APPROVAL', 'PENDING_USER_CONFIRMATION')`, statusToStore, req.AssignedTo, req.ResolutionDescription, req.ResolutionDescription, r.PathValue("ticketNumber"))
	if err != nil {
		log.Printf("Ticket update error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save ticket changes"})
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		var existingStatus string
		if err := tx.QueryRowContext(r.Context(), `SELECT status FROM tickets WHERE ticket_number = ?`, r.PathValue("ticketNumber")).Scan(&existingStatus); errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not verify ticket update"})
			return
		}
		if existingStatus == "CLOSED" || strings.HasPrefix(existingStatus, "PENDING_") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Closed tickets and tickets awaiting approval or confirmation cannot be edited"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Ticket update commit error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save ticket changes"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": statusToStore, "assigned_to": req.AssignedTo, "resolution_description": req.ResolutionDescription})
}

func (a *App) updateTicketPriority(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	if claims.Role != "ADMIN" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only administrators can change ticket priority"})
		return
	}
	var req TicketPriorityRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid priority update"})
		return
	}
	req.Priority = strings.ToUpper(strings.TrimSpace(req.Priority))
	if req.Priority != "LOW" && req.Priority != "MEDIUM" && req.Priority != "HIGH" && req.Priority != "CRITICAL" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ticket priority"})
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save ticket priority"})
		return
	}
	defer tx.Rollback()
	var status, assignedRole string
	var assignedTo sql.NullInt64
	if err := tx.QueryRowContext(r.Context(), `SELECT t.status, t.assigned_to, COALESCE(u.role, '') FROM tickets t LEFT JOIN users u ON u.id = t.assigned_to WHERE t.ticket_number = ? FOR UPDATE`, r.PathValue("ticketNumber")).Scan(&status, &assignedTo, &assignedRole); errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load ticket"})
		return
	}
	if status == "CLOSED" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Closed tickets cannot be edited"})
		return
	}
	if req.Priority == "CRITICAL" && (!assignedTo.Valid || assignedRole != "SUPPORT") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Assign this ticket to a support user before changing its priority to CRITICAL"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE tickets SET priority = ? WHERE ticket_number = ?`, req.Priority, r.PathValue("ticketNumber")); err != nil {
		log.Printf("Ticket priority update error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save ticket priority"})
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Ticket priority commit error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save ticket priority"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"priority": req.Priority})
}

func (a *App) updateTicketStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	var req TicketStatusRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid status update"})
		return
	}
	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))
	if req.Status != "OPEN" && req.Status != "ASSIGNED" && req.Status != "IN_PROGRESS" && req.Status != "RESOLVED" && req.Status != "CLOSED" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ticket status"})
		return
	}
	req.ResolutionDescription = strings.TrimSpace(req.ResolutionDescription)
	if claims.Role == "ADMIN" && req.Status == "RESOLVED" && req.ResolutionDescription == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "A resolution description is required to resolve this ticket"})
		return
	}
	var result sql.Result
	var err error
	number := r.PathValue("ticketNumber")
	statusToStore := req.Status
	switch claims.Role {
	case "ADMIN":
		if req.Status == "RESOLVED" {
			statusToStore = "PENDING_USER_CONFIRMATION"
		}
		result, err = a.db.ExecContext(r.Context(), `UPDATE tickets SET status = ?, resolution_description = CASE WHEN ? <> '' THEN ? ELSE resolution_description END WHERE ticket_number = ? AND status NOT IN ('CLOSED', 'PENDING_APPROVAL', 'PENDING_RESOLUTION_APPROVAL', 'PENDING_USER_CONFIRMATION')`, statusToStore, req.ResolutionDescription, req.ResolutionDescription, number)
	case "SUPPORT":
		if req.Status == "RESOLVED" || req.Status == "CLOSED" {
			var hasComment bool
			if err := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM comments c JOIN tickets t ON t.id = c.ticket_id WHERE t.ticket_number = ? AND c.user_id = ? AND c.created_at >= t.updated_at)`, number, claims.UserID).Scan(&hasComment); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not verify the resolution comment"})
				return
			}
			if !hasComment {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Post a comment explaining the work completed before resolving or requesting closure"})
				return
			}
		}
		if req.Status == "CLOSED" {
			statusToStore = "PENDING_APPROVAL"
		} else if req.Status == "RESOLVED" {
			statusToStore = "PENDING_RESOLUTION_APPROVAL"
		}
		// Enforce ownership in the UPDATE itself to avoid a reassignment race.
		result, err = a.db.ExecContext(r.Context(), `UPDATE tickets SET status = ?, resolution_description = CASE WHEN ? <> '' THEN ? ELSE resolution_description END WHERE ticket_number = ? AND assigned_to = ? AND status NOT IN ('CLOSED', 'PENDING_APPROVAL', 'PENDING_RESOLUTION_APPROVAL', 'PENDING_USER_CONFIRMATION')`, statusToStore, req.ResolutionDescription, req.ResolutionDescription, number, claims.UserID)
	default:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Your role cannot update ticket status"})
		return
	}
	if err != nil {
		log.Printf("Ticket status update error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not update ticket status"})
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		var stillAuthorized int
		check := `SELECT 1 FROM tickets WHERE ticket_number = ? AND status NOT IN ('CLOSED', 'PENDING_APPROVAL', 'PENDING_RESOLUTION_APPROVAL', 'PENDING_USER_CONFIRMATION')`
		checkArgs := []any{number}
		if claims.Role == "SUPPORT" {
			check += ` AND assigned_to = ?`
			checkArgs = append(checkArgs, claims.UserID)
		}
		if err := a.db.QueryRowContext(r.Context(), check, checkArgs...).Scan(&stillAuthorized); err == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": statusToStore, "resolution_description": req.ResolutionDescription})
			return
		}
		var blocked bool
		if err := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tickets WHERE ticket_number = ? AND (status = 'CLOSED' OR status LIKE 'PENDING_%'))`, number).Scan(&blocked); err == nil && blocked {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Closed tickets and tickets awaiting approval or confirmation cannot be edited"})
			return
		}
		message := "Ticket not found"
		if claims.Role == "SUPPORT" {
			message = "Ticket not found or not assigned to you"
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": message})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": statusToStore, "resolution_description": req.ResolutionDescription})
}

type ClosureReviewRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func (a *App) reviewTicketClosure(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	if claims.Role != "ADMIN" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only administrators can review closure requests"})
		return
	}
	var req ClosureReviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid closure review request"})
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Action != "approve" && req.Action != "reject" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Choose approve or reject"})
		return
	}
	if req.Action == "reject" && req.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "A reason is required when rejecting closure"})
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not review closure request"})
		return
	}
	defer tx.Rollback()
	var ticketID int64
	var status string
	if err := tx.QueryRowContext(r.Context(), `SELECT id, status FROM tickets WHERE ticket_number = ? FOR UPDATE`, r.PathValue("ticketNumber")).Scan(&ticketID, &status); errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load ticket"})
		return
	}
	if status != "PENDING_APPROVAL" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Ticket is not pending admin approval"})
		return
	}
	newStatus := "PENDING_USER_CONFIRMATION"
	if req.Action == "reject" {
		newStatus = "IN_PROGRESS"
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE tickets SET status = ? WHERE id = ?`, newStatus, ticketID); err != nil {
		log.Printf("Closure review update error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save closure decision"})
		return
	}
	if req.Action == "reject" {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO comments (ticket_id, user_id, comment_text) VALUES (?, ?, ?)`, ticketID, claims.UserID, "Closure rejected: "+req.Reason); err != nil {
			log.Printf("Closure rejection comment error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save rejection reason"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save closure decision"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}

func (a *App) reviewTicketResolution(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	if claims.Role != "ADMIN" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only administrators can review ticket resolutions"})
		return
	}
	var req ClosureReviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid resolution review request"})
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Action != "approve" && req.Action != "reject" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Choose approve or reject"})
		return
	}
	if req.Action == "reject" && req.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "A reason is required when rejecting the resolution"})
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not review ticket resolution"})
		return
	}
	defer tx.Rollback()
	var ticketID int64
	var status string
	if err := tx.QueryRowContext(r.Context(), `SELECT id, status FROM tickets WHERE ticket_number = ? FOR UPDATE`, r.PathValue("ticketNumber")).Scan(&ticketID, &status); errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load ticket"})
		return
	}
	if status != "PENDING_RESOLUTION_APPROVAL" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Ticket is not pending resolution approval"})
		return
	}
	newStatus := "PENDING_USER_CONFIRMATION"
	if req.Action == "reject" {
		newStatus = "IN_PROGRESS"
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE tickets SET status = ? WHERE id = ?`, newStatus, ticketID); err != nil {
		log.Printf("Resolution review update error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save resolution decision"})
		return
	}
	if req.Action == "reject" {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO comments (ticket_id, user_id, comment_text) VALUES (?, ?, ?)`, ticketID, claims.UserID, "Resolution rejected by Admin: "+req.Reason); err != nil {
			log.Printf("Resolution rejection comment error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save rejection reason"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save resolution decision"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}

func (a *App) confirmTicketResolution(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	if claims.Role != "USER" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only the ticket creator can confirm its resolution"})
		return
	}
	result, err := a.db.ExecContext(r.Context(), `UPDATE tickets SET status = 'CLOSED' WHERE ticket_number = ? AND created_by = ? AND status = 'PENDING_USER_CONFIRMATION'`, r.PathValue("ticketNumber"), claims.UserID)
	if err != nil {
		log.Printf("User resolution confirmation error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not confirm ticket resolution"})
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		var status string
		if err := a.db.QueryRowContext(r.Context(), `SELECT status FROM tickets WHERE ticket_number = ? AND created_by = ?`, r.PathValue("ticketNumber"), claims.UserID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not verify ticket"})
			return
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Ticket is not awaiting your resolution confirmation"})
		return
	}
	var finalStatus string
	if err := a.db.QueryRowContext(r.Context(), `SELECT status FROM tickets WHERE ticket_number = ? AND created_by = ?`, r.PathValue("ticketNumber"), claims.UserID).Scan(&finalStatus); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Ticket was confirmed but could not be reloaded"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": finalStatus})
}

func (a *App) assignTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	if claims.Role != "ADMIN" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only administrators can assign tickets"})
		return
	}
	var req TicketAssignmentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid assignment request"})
		return
	}
	if req.AssignedTo != nil {
		var isSupport int
		err := a.db.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE id = ? AND role = 'SUPPORT'`, *req.AssignedTo).Scan(&isSupport)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Tickets can only be assigned to a support user"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not validate support user"})
			return
		}
	} else {
		var priority string
		if err := a.db.QueryRowContext(r.Context(), `SELECT priority FROM tickets WHERE ticket_number = ?`, r.PathValue("ticketNumber")).Scan(&priority); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not validate ticket assignment"})
			}
			return
		}
		if priority == "CRITICAL" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Critical tickets must remain assigned to a support user"})
			return
		}
	}
	result, err := a.db.ExecContext(r.Context(), `UPDATE tickets SET assigned_to = ? WHERE ticket_number = ? AND status <> 'CLOSED'`, req.AssignedTo, r.PathValue("ticketNumber"))
	if err != nil {
		log.Printf("Ticket assignment error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not assign ticket"})
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		var exists int
		if err := a.db.QueryRowContext(r.Context(), `SELECT 1 FROM tickets WHERE ticket_number = ?`, r.PathValue("ticketNumber")).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Ticket not found"})
			return
		}
		var closed bool
		if err := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tickets WHERE ticket_number = ? AND status = 'CLOSED')`, r.PathValue("ticketNumber")).Scan(&closed); err == nil && closed {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Closed tickets cannot be edited"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assigned_to": req.AssignedTo})
}

func (a *App) queryTickets(r *http.Request, query string, args ...any) ([]TicketListItem, error) {
	rows, err := a.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tickets := make([]TicketListItem, 0)
	for rows.Next() {
		var ticket TicketListItem
		var assigned sql.NullInt64
		var resolution sql.NullString
		if err := rows.Scan(
			&ticket.ID, &ticket.TicketNumber, &ticket.Title, &ticket.Description,
			&ticket.CategoryID, &ticket.CategoryName, &ticket.Priority, &ticket.Status,
			&assigned, &ticket.AssignedToName, &ticket.CreatedBy, &ticket.CreatedByName,
			&resolution, &ticket.CreatedAt, &ticket.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if assigned.Valid {
			id := assigned.Int64
			ticket.AssignedTo = &id
		}
		if resolution.Valid {
			value := resolution.String
			ticket.ResolutionDescription = &value
		}
		tickets = append(tickets, ticket)
	}
	return tickets, rows.Err()
}

func (a *App) listCategories(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}
	_ = claims

	rows, err := a.db.QueryContext(r.Context(), `SELECT id, name, COALESCE(description, '') FROM categories ORDER BY name`)
	if err != nil {
		log.Printf("Category query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load categories"})
		return
	}
	defer rows.Close()

	categories := make([]Category, 0)
	for rows.Next() {
		var category Category
		if err := rows.Scan(&category.ID, &category.Name, &category.Description); err != nil {
			log.Printf("Category scan error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load categories"})
			return
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Category iteration error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load categories"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": categories})
}

func (a *App) createTicket(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}

	var req CreateTicketRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ticket request"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.Priority = strings.ToUpper(strings.TrimSpace(req.Priority))
	if req.Title == "" || req.Description == "" || req.CategoryID <= 0 || req.Priority == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Title, description, category, and priority are required"})
		return
	}
	if len(req.Title) > 255 || len(req.Description) > 2000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Title must be 255 characters or fewer and description 2,000 characters or fewer"})
		return
	}
	if req.Priority != "LOW" && req.Priority != "MEDIUM" && req.Priority != "HIGH" && req.Priority != "CRITICAL" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Priority must be LOW, MEDIUM, HIGH, or CRITICAL"})
		return
	}

	var categoryExists int
	if err := a.db.QueryRowContext(r.Context(), `SELECT 1 FROM categories WHERE id = ?`, req.CategoryID).Scan(&categoryExists); errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Selected category does not exist"})
		return
	} else if err != nil {
		log.Printf("Category validation error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not validate the selected category"})
		return
	}

	status := "OPEN"
	var assignedTo *int64
	if req.Priority == "CRITICAL" {
		var supportID int64
		err := a.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE role = 'SUPPORT' ORDER BY id LIMIT 1`).Scan(&supportID)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "A critical ticket requires an active support user for immediate assignment, but no support user is available"})
			return
		}
		if err != nil {
			log.Printf("Support user lookup error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not assign the critical ticket"})
			return
		}
		assignedTo = &supportID
		status = "ASSIGNED"
	}

	// The named MySQL lock keeps sequential ticket-number allocation safe when
	// multiple requests arrive at once. It is connection-scoped and released below.
	conn, err := a.db.Conn(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Ticket creation is busy; please try again"})
		return
	}
	defer conn.Close()
	var lockAcquired sql.NullInt64
	if err := conn.QueryRowContext(r.Context(), `SELECT GET_LOCK('helpdesk_ticket_number', 10)`).Scan(&lockAcquired); err != nil || !lockAcquired.Valid || lockAcquired.Int64 != 1 {
		log.Printf("Ticket number lock error: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Ticket creation is busy; please try again"})
		return
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), `SELECT RELEASE_LOCK('helpdesk_ticket_number')`) }()

	year := time.Now().Year()
	prefix := fmt.Sprintf("HD-%d-", year)
	var sequence int64
	if err := conn.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(CAST(SUBSTRING(ticket_number, ?) AS UNSIGNED)), 0) + 1 FROM tickets WHERE ticket_number LIKE ?`, len(prefix)+1, prefix+"%").Scan(&sequence); err != nil {
		log.Printf("Ticket number generation error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not generate a ticket number"})
		return
	}
	ticketNumber := fmt.Sprintf("%s%04d", prefix, sequence)

	result, err := conn.ExecContext(r.Context(), `
		INSERT INTO tickets (ticket_number, title, description, category_id, priority, status, assigned_to, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, ticketNumber, req.Title, req.Description, req.CategoryID, req.Priority, status, assignedTo, claims.UserID)
	if err != nil {
		log.Printf("Ticket insert error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not save the ticket"})
		return
	}
	ticketID, err := result.LastInsertId()
	if err != nil {
		log.Printf("Ticket ID lookup error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Ticket was saved but could not be loaded"})
		return
	}

	var ticket TicketResponse
	var nullableAssigned sql.NullInt64
	var nullableResolution sql.NullString
	if err := conn.QueryRowContext(r.Context(), `
		SELECT t.id, t.ticket_number, t.title, t.description, t.category_id, t.priority, t.status,
		       t.assigned_to, t.created_by, t.resolution_description, t.created_at, t.updated_at,
		       u.id, u.full_name, u.email, u.role
		FROM tickets t JOIN users u ON u.id = t.created_by WHERE t.id = ?
	`, ticketID).Scan(
		&ticket.ID, &ticket.TicketNumber, &ticket.Title, &ticket.Description, &ticket.CategoryID,
		&ticket.Priority, &ticket.Status, &nullableAssigned, &ticket.CreatedBy,
		&nullableResolution, &ticket.CreatedAt, &ticket.UpdatedAt,
		&ticket.Creator.ID, &ticket.Creator.FullName, &ticket.Creator.Email, &ticket.Creator.Role,
	); err != nil {
		log.Printf("Created ticket readback error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Ticket was saved but could not be loaded"})
		return
	}
	if nullableAssigned.Valid {
		assignedID := nullableAssigned.Int64
		ticket.AssignedTo = &assignedID
	}
	if nullableResolution.Valid {
		resolution := nullableResolution.String
		ticket.ResolutionDescription = &resolution
	}
	writeJSON(w, http.StatusCreated, ticket)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid login request"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Email and password are required"})
		return
	}

	var user User
	var passwordHash string
	query := "SELECT id, full_name, email, `password`, role FROM users WHERE email = ?"
	err := a.db.QueryRowContext(r.Context(), query, req.Email).Scan(
		&user.ID, &user.FullName, &user.Email, &passwordHash, &user.Role,
	)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid email or password"})
		return
	}
	if err != nil {
		log.Printf("Login database error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Login is temporarily unavailable"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)) != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid email or password"})
		return
	}

	lifetime := 12 * time.Hour
	if req.RememberMe {
		lifetime = 30 * 24 * time.Hour
	}
	now := time.Now()
	sessionID, err := newSessionID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not create session"})
		return
	}
	claims := Claims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        sessionID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(lifetime)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.jwtSecret)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not create session"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":      "Login successful",
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int64(lifetime.Seconds()),
		"user":         user,
	})
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}

	var user User
	err := a.db.QueryRowContext(r.Context(), `
		SELECT id, full_name, email, role FROM users WHERE id = ?
	`, claims.UserID).Scan(&user.ID, &user.FullName, &user.Email, &user.Role)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "User account not found"})
		return
	}
	if err != nil {
		log.Printf("User lookup error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load user details"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]User{"user": user})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := a.authenticatedClaims(w, r)
	if !ok {
		return
	}

	a.revokedMu.Lock()
	a.revokedSessions[claims.ID] = claims.ExpiresAt.Time
	a.removeExpiredSessions(time.Now())
	a.revokedMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"message": "Logged out successfully"})
}

func (a *App) authenticatedClaims(w http.ResponseWriter, r *http.Request) (*Claims, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
		return nil, false
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(parts[1], claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return a.jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.UserID <= 0 || claims.ID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired session"})
		return nil, false
	}

	a.revokedMu.Lock()
	a.removeExpiredSessions(time.Now())
	_, revoked := a.revokedSessions[claims.ID]
	a.revokedMu.Unlock()
	if revoked {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Session has been logged out"})
		return nil, false
	}

	return claims, true
}

func (a *App) removeExpiredSessions(now time.Time) {
	for id, expiresAt := range a.revokedSessions {
		if !expiresAt.After(now) {
			delete(a.revokedSessions, id)
		}
	}
}

func newSessionID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("Could not write JSON response: %v", err)
	}
}

func (a *App) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if a.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isAllowedOrigin keeps configured production/development origins explicit,
// while allowing Angular's local dev server to choose an available port.
func (a *App) isAllowedOrigin(origin string) bool {
	if a.allowedOrigins[origin] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "127.0.0.1"
}
