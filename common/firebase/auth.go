package auth

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"

	firebase "firebase.google.com/go"
	"firebase.google.com/go/auth"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/option"
)

func InitFirebase() (*firebase.App, error) {
	// opt := option.WithCredentialsFile("env/ccpd-system-firebase-adminsdk-te9cz-5284634b36.json")

	// create client option
	// grab secret json encoded into base64
	firebaseJSON := os.Getenv("FIREBASE_JSON")
	firebaseDecode, _ := base64.StdEncoding.DecodeString(firebaseJSON)

	opt := option.WithCredentialsJSON(firebaseDecode)
	// init app
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		return nil, err
	}

	return app, err
}

func FirebaseAuthMiddleware(client *auth.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")

		// Verify the ID token
		decodedToken, err := client.VerifyIDToken(c, token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// log.Printf("Verified ID token: %v\n", decodedToken)
		c.Set("uid", decodedToken.UID)
		c.Next()
	}
}
