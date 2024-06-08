package appointment

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	// "github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// var validate = validator.New()

type PageContent struct {
	VideoLink       string `json:"link" binding:"required" validate:"required"`
	BookingLink     string `json:"bookingLink" binding:"required" validate:"required"`
	BannerImageName string `json:"BannerLink" binding:"required" validate:"required"`
}

func GetPageContent(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		// pull settings from mongo
		var result bson.M
		err := collection.FindOne(
			context.TODO(),
			bson.M{"type": "setting"},
			nil,
		).Decode(&result)
		if err != nil {
			c.String(http.StatusNotFound, "Item Not Found!")
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

// page content is an array of ContentInfo
type ContentInfo struct {
	Name string            `json:"name" binding:"required" validate:"required"`
	Data map[string]string `json:"data"`
}

func SetPageContent(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.TODO()
		// bind json
		var body []ContentInfo

		bindErr := c.ShouldBindJSON(&body)
		if bindErr != nil {
			fmt.Println(bindErr)
			c.String(http.StatusBadRequest, "Please Check Your Inputs!")
			return
		}

		// // validate with validtor
		// if err := validate.Struct(body); err != nil {
		// 	fmt.Println(err)
		// 	c.String(http.StatusBadRequest, "Validation error: "+err.Error())
		// 	return
		// }

		// update current page content
		setMsg, err := collection.UpdateOne(
			ctx,
			bson.M{"type": "setting"},
			bson.M{
				"$set": bson.M{
					"contentObj": body,
				},
			},
		)
		if err != nil {
			c.String(http.StatusInternalServerError, "Cannot Update Document")
			return
		}

		c.JSON(http.StatusOK, setMsg)
	}
}
