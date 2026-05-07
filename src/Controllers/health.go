package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const healthTimeLayout = "2006-01-02 15:04:05"

func InitHealthBroadcaster() {
	Services.SetWSConnectionCounter(func() int {
		return Websockets.MainHub.ConnectionCount()
	})
	Services.SetHealthBroadcaster(func(update Services.HealthUpdate) {
		subscribers := Services.GetHealthSubscribers()
		if len(subscribers) == 0 {
			return
		}
		Websockets.MainHub.BroadcastToUsers(subscribers, map[string]interface{}{
			"type": "health_update",
			"data": update,
		})
	})
}

func GetHealthData(c *gin.Context) {
	now := time.Now()
	start := now.Add(-time.Hour)
	end := now

	if s := c.Query("start"); s != "" {
		t, err := time.ParseInLocation(healthTimeLayout, s, time.Local)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "invalid start format, expected: 2006-01-02 15:04:05"})
			c.Abort()
			return
		}
		start = t
	}

	if e := c.Query("end"); e != "" {
		t, err := time.ParseInLocation(healthTimeLayout, e, time.Local)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "invalid end format, expected: 2006-01-02 15:04:05"})
			c.Abort()
			return
		}
		end = t
	}

	if end.Before(start) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "end must be after start"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, Services.ReadHealthData(start, end))
}
