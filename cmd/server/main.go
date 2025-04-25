package main

import (
	"log"
	"net/http"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/api"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
	"github.com/gin-gonic/gin"
)

func main() {
    r := gin.Default()
    if err := db.Init(); err != nil {
        log.Fatalf("failed to initialize db: %v", err)
    }
    queries := database.New(db.DB)

    subHandler := &api.SubscriptionHandler{
        Queries: queries,
    }

    r.GET("/healthz", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"status": "ok"})
    })

    api.RegisterSubscriptionRoutes(r, subHandler)

    // TODO: Add other API and UI routes here

    r.Run(":8080") 
}