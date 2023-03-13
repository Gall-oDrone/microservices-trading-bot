package controllers

import (
	"fmt"
	"net/http"
)

func SignUp(w http.ResponseWriter, r *http.Request) {
	fmt.Println(w)
}
func TokenValidation(w http.ResponseWriter, r *http.Request) {
	fmt.Println(w)
}
func CodeValidation(w http.ResponseWriter, r *http.Request) {
	fmt.Println(w)
}
