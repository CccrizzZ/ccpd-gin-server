package auth

import (
	"context"

	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"
)

func initFirebase() *firebase.App {
	// create client option
	opt := option.WithCredentialsFile("../env/ccpd-system-firebase-adminsdk-te9cz-7be0e6279c.json")

	// init app
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		return nil
	}
	return app
}
