package main

type userModel struct {
	ID    int    `json:"id,omitempty"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type configModel struct {
	dbHost string
	dbPort string
	dbName string
	dbUser string
	dbPass string
	host   string
	port   string
}
