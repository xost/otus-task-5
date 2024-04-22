package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	createUserTpl  = `INSERT INTO users (email, name) VALUES ($1, $2)`
	getUserTpl     = `SELECT email, name FROM users WHERE id=$1`
	getUserListTpl = `SELECT id, email, name FROM users`
	updateUserTpl  = `UPDATE users SET email=$2, name=$3 WHERE id=$1`
	deleteUserTpl  = `DELETE FROM users WHERE id=$1`
)

var (
	createUserStmt  *sql.Stmt
	getUserStmt     *sql.Stmt
	getUserListStmt *sql.Stmt
	updateUserStmt  *sql.Stmt
	deleteUserStmt  *sql.Stmt
)

func readConf() *configModel {
	cfg := &configModel{
		dbHost: "localhost",
		dbPort: "5432",
		dbName: "db",
		dbUser: "root",
		dbPass: "password",
		host:   "localhost",
		port:   "8000",
	}
	dbHost := os.Getenv("DBHOST")
	dbPort := os.Getenv("DBPORT")
	dbName := os.Getenv("DBNAME")
	dbUser := os.Getenv("DBUSER")
	dbPass := os.Getenv("DBPASS")
	host := os.Getenv("HOST")
	port := os.Getenv("PORT")
	if dbHost != "" {
		cfg.dbHost = dbHost
	}
	if dbPort != "" {
		cfg.dbPort = dbPort
	}
	if dbName != "" {
		cfg.dbName = dbName
	}
	if dbUser != "" {
		cfg.dbUser = dbUser
	}
	if dbPass != "" {
		cfg.dbPass = dbPass
	}
	if host != "" {
		cfg.host = host
	}
	if port != "" {
		cfg.port = port
	}
	return cfg
}

func makeDBURL(cfg *configModel) (*sql.DB, error) {
	pgConn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.dbHost, cfg.dbPort, cfg.dbUser, cfg.dbPass, cfg.dbName,
	)
	db, err := sql.Open("postgres", pgConn)
	return db, err
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := readConf()

	db, err := makeDBURL(cfg)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	if err = db.PingContext(ctx); err != nil {
		log.Fatal("Failed to check db connection:", err)
	}

	mustPrepareStmts(ctx, db)

	r := mux.NewRouter()
	r.Use(prometheusMiddleware)

	// r.HandleFunc("/", indexHandle).Methods("GET")
	r.HandleFunc("/api/v1", createUserHandle).Methods("POST")
	r.HandleFunc("/api/v1", getUserHandle).Methods("GET")
	r.HandleFunc("/api/v1/{id}", getUserHandle).Methods("GET")
	r.HandleFunc("/api/v1/{id}", updateUserHandle).Methods("PUT")
	r.HandleFunc("/api/v1/{id}", deleteUserHandle).Methods("DELETE")
	r.Handle("/metrics", promhttp.Handler())

	bindOn := fmt.Sprintf("%s:%s", cfg.host, cfg.port)
	if err := http.ListenAndServe(bindOn, r); err != nil {
		log.Printf("Failed to bind on [%s]: %s", bindOn, err)
	}
}

func mustPrepareStmts(ctx context.Context, db *sql.DB) {
	var err error

	createUserStmt, err = db.PrepareContext(ctx, createUserTpl)
	if err != nil {
		panic(err)
	}

	getUserStmt, err = db.PrepareContext(ctx, getUserTpl)
	if err != nil {
		panic(err)
	}

	getUserListStmt, err = db.PrepareContext(ctx, getUserListTpl)
	if err != nil {
		panic(err)
	}

	updateUserStmt, err = db.PrepareContext(ctx, updateUserTpl)
	if err != nil {
		panic(err)
	}

	deleteUserStmt, err = db.PrepareContext(ctx, deleteUserTpl)
	if err != nil {
		panic(err)
	}
}

func indexHandle(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("hello... \n"))
}

func createUserHandle(w http.ResponseWriter, r *http.Request) {
	u := &userModel{}
	if err := json.NewDecoder(r.Body).Decode(u); err != nil {
		log.Println("Failed to parse user data:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Failed to parse user data"))
		return
	}
	if err := createUser(u); err != nil {
		log.Println("Failed to create new user:", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to create new user"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "User with email=%s was created", u.Email)
	log.Printf("User with email=%s was created", (*u).Email)
}

func getUserHandle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if id, ok := vars["id"]; ok {
		idd, err := strconv.Atoi(id)
		if err != nil {
			log.Println("Failed to parse user id (must be an integer):", err)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Failed to parse user id (must be an integer)"))
			return
		}
		u, err := getUser(idd)
		if err != nil {
			log.Println("Failed to get user:", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to get user"))
			return
		}
		data, _ := json.Marshal(u)
		w.WriteHeader(http.StatusOK)
		w.Header().Add("Content-Type", "application/json")
		w.Write(data)
		log.Printf("Got user: %s", string(data))
		return
	}
	ul, err := getUserList()
	if err != nil {
		log.Println("Failed to get user list:", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to get user list"))
		return
	}
	data, _ := json.Marshal(ul)
	log.Printf("Got user list: %s", string(data))
	w.WriteHeader(http.StatusOK)
	w.Header().Add("Content-Type", "application/json")
	w.Write(data)
}

func updateUserHandle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		log.Println("User id is required")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("User id is required"))
		return
	}
	u := &userModel{}
	if err := json.NewDecoder(r.Body).Decode(u); err != nil {
		log.Println("Failed to parse user data:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Failed to parse user data"))
		return
	}
	idd, err := strconv.Atoi(id)
	if err != nil {
		log.Println("Failed to parse user id (must be an integer):", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Failed to parse user id (must be an integer)"))
		return
	}
	u.ID = idd
	if err := updateUser(u); err != nil {
		log.Println("Failed to update user:", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to update user"))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Updated user with id=%d", idd)
	log.Printf("Updated user : %+v", *u)
}

func deleteUserHandle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		log.Println("User id is required")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("User id is required"))
		return
	}
	idd, err := strconv.Atoi(id)
	if err != nil {
		log.Println("Failed to parse user id (must be an integer):", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Failed to parse user id (must be an integer)"))
		return
	}
	if err := deleteUser(id); err != nil {
		log.Println("Failed to delete user:", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to delete user"))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "User [id=%d] was deleted", idd)
	log.Printf("User [id=%d] was deleted", idd)
}

func createUser(u *userModel) error {
	if _, err := createUserStmt.Exec(u.Email, u.Name); err != nil {
		return err
	}
	return nil
}

func getUser(id int) (*userModel, error) {
	rows, err := getUserStmt.Query(id)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		return nil, errors.New("there is no user with specified id")
	}

	email := new(string)
	name := new(string)

	if err = rows.Scan(email, name); err != nil {
		return nil, err
	}
	return &userModel{
		ID:    id,
		Email: *email,
		Name:  *name,
	}, nil
}

func getUserList() ([]userModel, error) {
	rows, err := getUserListStmt.Query()
	if err != nil {
		return nil, err
	}
	id := new(int)
	name := new(string)
	email := new(string)
	ul := make([]userModel, 0)
	for rows.Next() {
		if err = rows.Scan(id, email, name); err != nil {
			continue
		}
		ul = append(ul, userModel{
			ID:    *id,
			Email: *email,
			Name:  *name,
		})
	}
	if len(ul) == 0 {
		return nil, errors.New("where is no any users")
	}
	return ul, nil
}

func updateUser(u *userModel) error {
	if _, err := updateUserStmt.Exec(u.ID, u.Email, u.Name); err != nil {
		return err
	}
	return nil
}

func deleteUser(email string) error {
	res, err := deleteUserStmt.Exec(email)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("user does not exist")
	}
	return nil
}

// type responseWriter struct {
// 	http.ResponseWriter
// 	statusCode int
// }
//
// func NewResponseWriter(w http.ResponseWriter) *responseWriter {
// 	return &responseWriter{w, http.StatusOK}
// }
//
// func (rw *responseWriter) WriteHeader(code int) {
// 	rw.statusCode = code
// 	rw.ResponseWriter.WriteHeader(code)
// }
