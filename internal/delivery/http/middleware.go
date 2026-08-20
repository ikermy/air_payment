package http

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

const userIDLocalsKey = "userID"

// userIDMiddleware validates uid and makes the parsed user ID available to the handler.
func userIDMiddleware(c *fiber.Ctx) error {
	uidStr := c.Query("uid")
	if uidStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "uid is required"})
	}

	uid, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid uid"})
	}

	c.Locals(userIDLocalsKey, uint32(uid))
	return c.Next()
}

func getUserID(c *fiber.Ctx) (uint32, bool) {
	userID, ok := c.Locals(userIDLocalsKey).(uint32)
	return userID, ok
}
