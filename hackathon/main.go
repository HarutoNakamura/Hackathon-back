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

	"github.com/go-sql-driver/mysql"
)

var db *sql.DB

type Post struct {
	ID        int    `json:"id"`
	Email     string `json:"email"`
	Content   string `json:"content"`
	LikeCount int    `json:"like_count"`
}

type Reply struct {
	PostID  int    `json:"post_id"`
	Email   string `json:"email"`
	Content string `json:"content"`
}

func main() {
	// TLS設定
	rootCert := "./server-ca.pem"
	clientCert := "./client-cert.pem"
	clientKey := "./client-key.pem"

	err := RegisterTLSConfig("custom", rootCert, clientCert, clientKey)
	if err != nil {
		log.Fatalf("Failed to register TLS config: %v", err)
	}

	// データベース接続
	dsn := fmt.Sprintf("root:password@tcp(your-db-host:3306)/hackathon?tls=custom")
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// ルーティング
	http.HandleFunc("/api/posts/create", corsMiddleware(createPostHandler))
	http.HandleFunc("/api/posts/get", corsMiddleware(getPostsHandler))
	http.HandleFunc("/api/replies/add", corsMiddleware(addReplyHandler))
	http.HandleFunc("/api/likes/toggle", corsMiddleware(toggleLikeHandler))

	log.Println("Server is running on port 8081")
	http.ListenAndServe(":8081", nil)
}

// 投稿作成
func createPostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var post Post
	if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("INSERT INTO posts (email, content) VALUES (?, ?)", post.Email, post.Content)
	if err != nil {
		log.Printf("Database error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// 投稿取得
func getPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query(`
		SELECT posts.id, posts.email, posts.content, COUNT(likes.id) AS like_count
		FROM posts
		LEFT JOIN likes ON posts.id = likes.post_id
		GROUP BY posts.id
		ORDER BY posts.created_at DESC
	`)
	if err != nil {
		log.Printf("Database error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var post Post
		if err := rows.Scan(&post.ID, &post.Email, &post.Content, &post.LikeCount); err != nil {
			log.Printf("Row scan error: %v", err)
			http.Error(w, "Row scan error", http.StatusInternalServerError)
			return
		}
		posts = append(posts, post)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(posts)
}

// リプライ追加
func addReplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var reply Reply
	if err := json.NewDecoder(r.Body).Decode(&reply); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("INSERT INTO replies (post_id, email, content) VALUES (?, ?, ?)", reply.PostID, reply.Email, reply.Content)
	if err != nil {
		log.Printf("Database error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// いいねトグル
func toggleLikeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		PostID int    `json:"post_id"`
		Email  string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	// いいねの存在を確認
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM likes WHERE post_id = ? AND email = ?)", data.PostID, data.Email).Scan(&exists)
	if err != nil {
		log.Printf("Database error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if exists {
		// いいね削除
		_, err = db.Exec("DELETE FROM likes WHERE post_id = ? AND email = ?", data.PostID, data.Email)
	} else {
		// いいね追加
		_, err = db.Exec("INSERT INTO likes (post_id, email) VALUES (?, ?)", data.PostID, data.Email)
	}

	if err != nil {
		log.Printf("Database error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
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

// TLS設定
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
		RootCAs:      rootCertPool,
		Certificates: []tls.Certificate{clientCertPair},
	})
	return nil
}
