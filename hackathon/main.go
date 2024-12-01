package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/go-sql-driver/mysql"
)

var db *sql.DB

type Comment struct {
	Email   string `json:"email"`
	Comment string `json:"comment"`
}

func main() {
	// TLS証明書の設定
	rootCert := "./server-ca.pem"
	clientCert := "./client-cert.pem"
	clientKey := "./client-key.pem"

	err := RegisterTLSConfig("custom", rootCert, clientCert, clientKey)
	if err != nil {
		log.Fatalf("Failed to register TLS config: %v", err)
	}

	// データベース接続設定
	dsn := fmt.Sprintf("root:rxVqTvN7XkP5UZ@cloudsql(%s)/hackathon", os.Getenv("DATABASE_HOST"))
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// ルーティング
	http.HandleFunc("/api/comments/post", corsMiddleware(postCommentHandler))
	http.HandleFunc("/api/comments/get", corsMiddleware(getCommentsHandler))

	log.Println("Backend server is running on port 8081")
	log.Println(db.Query("SHOW TABLES"))
	http.ListenAndServe(":8081", nil)
}

// コメント投稿処理
func postCommentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var comment Comment
	if err := json.NewDecoder(r.Body).Decode(&comment); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("INSERT INTO comments (email, comment) VALUES (?, ?)", comment.Email, comment.Comment)
	if err != nil {
		log.Printf("Database error: %v", err) // Log database error for debugging
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("Comment added"))
}

// コメント取得処理
func getCommentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query("SELECT email, comment FROM comments ORDER BY id DESC")
	if err != nil {
		log.Printf("Database error: %v", err) // Log database error for debugging
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var comment Comment
		if err := rows.Scan(&comment.Email, &comment.Comment); err != nil {
			log.Printf("Row scan error: %v", err) // Log row scan error
			http.Error(w, "Row scan error", http.StatusInternalServerError)
			return
		}
		comments = append(comments, comment)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(comments)
}

// CORSミドルウェア
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// TLS設定の登録
func RegisterTLSConfig(name, rootCert, clientCert, clientKey string) error {
	rootCertPool := x509.NewCertPool()
	pem, err := ioutil.ReadFile(rootCert)
	if err != nil {
		return fmt.Errorf("Failed to read root cert: %w", err)
	}
	if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
		return fmt.Errorf("Failed to append root cert")
	}

	clientCertPair, err := tls.LoadX509KeyPair(clientCert, clientKey)
	if err != nil {
		return fmt.Errorf("Failed to load client cert/key pair: %w", err)
	}

	mysql.RegisterTLSConfig(name, &tls.Config{
		RootCAs:            rootCertPool,
		Certificates:       []tls.Certificate{clientCertPair},
		InsecureSkipVerify: true,
	})
	return nil
}
