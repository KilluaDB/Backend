package responses

import "github.com/gin-gonic/gin"

type APIResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func JSON(c *gin.Context, statusCode int, status string, data interface{}, message string, err error) {
	response := APIResponse{
		Status:  status,
		Message: message,
		Data:    data,
	}

	if err != nil {
		response.Error = err.Error()
	}

	c.JSON(statusCode, response)
}

func Success(c *gin.Context, statusCode int, data interface{}, message string) {
	c.JSON(statusCode, APIResponse{
		Status:  "success",
		Message: message,
		Data:    data,
	})
}

func Fail(c *gin.Context, statusCode int, err error, message string) {
	resp := APIResponse{
		Status:  "error",
		Message: message,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	c.JSON(statusCode, resp)
}
