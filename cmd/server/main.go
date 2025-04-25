package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
    r := gin.Default()

    // Health check endpoint
    r.GET("/healthz", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"status": "ok"})
    })

    // TODO: Add API and UI routes here

    r.Run(":8080") // Listen and serve on 0.0.0.0:8080
}