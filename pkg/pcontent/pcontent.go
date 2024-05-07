package appointment

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type LinkJson struct {
	Link string `json:"link" binding:"required" validate:"required"`
}

var validate = validator.New()

func GetPageContent(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		// query options
		opts := options.FindOne().SetSort(bson.D{{Key: "time", Value: -1}})

		// pull settings from mongo
		var result bson.M
		err := collection.FindOne(
			context.TODO(),
			bson.M{"type": "setting"},
			opts,
		).Decode(&result)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"data": "Item Not Found!"})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func SetPageContent(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.TODO()
		// bind json
		var request LinkJson
		bindErr := c.ShouldBindJSON(&request)
		if bindErr != nil {
			c.String(http.StatusBadRequest, "Please Check Your Inputs!")
			return
		}

		// validate with validtor
		if err := validate.Struct(request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation error: " + err.Error()})
			return
		}

		// update current lot
		setMsg, err := collection.UpdateOne(
			ctx,
			bson.M{"type": "setting"},
			bson.M{"$set": bson.M{"currentLink": request.Link}},
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"data": "Cannot Update Document"})
			return
		}

		c.JSON(http.StatusOK, setMsg)
	}
}
