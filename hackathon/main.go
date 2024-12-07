package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"cloud.google.com/go/vertexai/genai"
	"github.com/go-sql-driver/mysql"
	"google.golang.org/api/option"
)

var db *sql.DB

const (
	location  = "asia-northeast1"
	modelName = "gemini-1.5-flash-002"
	projectID = "term6-haruto-nakamura-441801"
)

func main() {
	//TLS証明書の設定
	rootCert := "./server-ca.pem"
	clientCert := "./client-cert.pem"
	clientKey := "./client-key.pem"

	err := RegisterTLSConfig("custom", rootCert, clientCert, clientKey)
	if err != nil {
		log.Fatalf("Failed to register TLS config: %v", err)
	}

	// データベース接続設定
	dsn := fmt.Sprintf("root:rxVqTvN7XkP5UZ@tcp(35.226.119.65:3306)/hackathon?tls=custom")
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// ルーティング
	http.HandleFunc("/api/posts/create", corsMiddleware(postHandler))
	http.HandleFunc("/api/posts/get", corsMiddleware(getPostsHandler))
	http.HandleFunc("/api/replies/create", corsMiddleware(replyHandler))
	http.HandleFunc("/api/replies/get", corsMiddleware(getRepliesHandler))
	http.HandleFunc("/api/likes/add", corsMiddleware(likeHandler))
	http.HandleFunc("/api/likes/get", corsMiddleware(getLikesHandler))
	http.HandleFunc("/api/posts/filter", corsMiddleware(filterPostsHandler))

	log.Println("Backend server is running on port 8081")
	http.ListenAndServe(":8081", nil)
}

func createVertexAIClient(ctx context.Context, projectID, location string) (*genai.Client, error) {
	credentialsJSON := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if credentialsJSON == "" {
		log.Fatal("GOOGLE_APPLICATION_CREDENTIALS environment variable is not set or empty")
	}
	log.Print([]byte(credentialsJSON))
	//client, err := genai.NewClient(ctx, projectID, location, option.WithCredentialsJSON(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")))
	client, err := genai.NewClient(ctx, projectID, location, option.WithCredentialsJSON([]byte(credentialsJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Vertex AI client: %w", err)
	}
	return client, nil
}

func filterPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var input struct {
		Topic string `json:"topic"`
		Posts []struct {
			ID      int    `json:"id"`
			Content string `json:"content"`
		} `json:"posts"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	// Create Vertex AI client
	ctx := context.Background()
	client, err := createVertexAIClient(ctx, projectID, location)
	if err != nil {
		log.Printf("Error initializing Vertex AI client: %v", err)
		http.Error(w, "AI client initialization failed", http.StatusInternalServerError)
		return
	}

	gemini := client.GenerativeModel(modelName)
	var relevantPostIDs []int

	for _, post := range input.Posts {
		prompt := fmt.Sprintf(
			"次の文章は「%s」と関連がありますか？関連があれば'yes'とだけ答え、なければ'no'とだけ答えてください。\n%s",
			input.Topic,
			post.Content,
		)
		response, err := gemini.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			log.Printf("Error generating content from Gemini: %v", err)
			http.Error(w, "AI inference failed", http.StatusInternalServerError)
			return
		}

		log.Printf("Prompt sent to Gemini: %s", prompt)
		if len(response.Candidates) > 0 && len(response.Candidates[0].Content.Parts) > 0 {
			if text, ok := response.Candidates[0].Content.Parts[0].(genai.Text); ok {
				if text == "yes\n" {
					relevantPostIDs = append(relevantPostIDs, post.ID)
				} else if text != "no\n" {
					log.Printf("Unexpected AI response: %v", text)
				}
			} else {
				log.Printf("AI response is not text: %v", response.Candidates[0].Content.Parts[0])
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(relevantPostIDs)
}

func postHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var post struct {
		Email   string `json:"email"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("INSERT INTO posts (email, content) VALUES (?, ?)", post.Email, post.Content)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("Post added"))
}

func replyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var reply struct {
		PostID  int    `json:"post_id"`
		Email   string `json:"email"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reply); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("INSERT INTO replies (post_id, email, content) VALUES (?, ?, ?)", reply.PostID, reply.Email, reply.Content)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("Reply added"))
}

func getPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	query := `
		SELECT 
			posts.id, posts.email, posts.content, posts.created_at, 
			IFNULL(like_counts.like_count, 0) AS like_count 
		FROM posts
		LEFT JOIN (
			SELECT post_id, COUNT(*) AS like_count
			FROM likes
			GROUP BY post_id
		) AS like_counts
		ON posts.id = like_counts.post_id
		ORDER BY posts.created_at DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var posts []struct {
		ID        int    `json:"id"`
		Email     string `json:"email"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
		Likes     int    `json:"likes"`
	}

	for rows.Next() {
		var post struct {
			ID        int    `json:"id"`
			Email     string `json:"email"`
			Content   string `json:"content"`
			CreatedAt string `json:"created_at"`
			Likes     int    `json:"likes"`
		}
		if err := rows.Scan(&post.ID, &post.Email, &post.Content, &post.CreatedAt, &post.Likes); err != nil {
			http.Error(w, "Row scan error", http.StatusInternalServerError)
			return
		}
		posts = append(posts, post)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(posts)
}

func getRepliesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	postID := r.URL.Query().Get("post_id")
	rows, err := db.Query("SELECT email, content, created_at FROM replies WHERE post_id = ? ORDER BY created_at DESC", postID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var replies []struct {
		Email     string `json:"email"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
	}

	for rows.Next() {
		var reply struct {
			Email     string `json:"email"`
			Content   string `json:"content"`
			CreatedAt string `json:"created_at"`
		}
		if err := rows.Scan(&reply.Email, &reply.Content, &reply.CreatedAt); err != nil {
			http.Error(w, "Row scan error", http.StatusInternalServerError)
			return
		}
		replies = append(replies, reply)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(replies)
}

func likeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var like struct {
		PostID int    `json:"post_id"`
		Email  string `json:"email"`
	}

	if err := json.NewDecoder(r.Body).Decode(&like); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	// いいねが存在するか確認
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM likes WHERE post_id = ? AND email = ?)", like.PostID, like.Email).Scan(&exists)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if exists {
		// 存在する場合は削除
		_, err := db.Exec("DELETE FROM likes WHERE post_id = ? AND email = ?", like.PostID, like.Email)
		if err != nil {
			http.Error(w, "Failed to remove like", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("Like removed"))
	} else {
		// 存在しない場合は追加
		_, err := db.Exec("INSERT INTO likes (post_id, email) VALUES (?, ?)", like.PostID, like.Email)
		if err != nil {
			http.Error(w, "Failed to add like", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("Like added"))
	}
}

func getLikesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	postID := r.URL.Query().Get("post_id")
	row := db.QueryRow("SELECT COUNT(*) FROM likes WHERE post_id = ?", postID)

	var likeCount int
	if err := row.Scan(&likeCount); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"like_count": likeCount})
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
		InsecureSkipVerify: false,
	})
	return nil
}
