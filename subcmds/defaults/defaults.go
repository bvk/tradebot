// Copyright (c) 2025 BVK Chaitanya

package defaults

import (
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

func ServerPort() int {
	const defaultValue = 10000

	value := os.Getenv("TRADEBOT_SERVER_PORT")
	if len(value) == 0 {
		return defaultValue
	}

	port, err := strconv.ParseInt(value, 10, 16)
	if err != nil {
		log.Printf("TRADEBOT_SERVER_PORT value must be an decimal integer (value %s is ignored)", value)
		return defaultValue
	}
	return int(port)
}

func DataDir() string {
	const fallbackValue = "."
	user, err := user.Current()
	if err != nil {
		log.Printf("could not query for current user (using fallback data directory): %v", err)
		return fallbackValue
	}
	if len(user.HomeDir) == 0 {
		log.Printf("could not find home directory; using fallback data directory")
		return fallbackValue
	}

	var defaultValue = filepath.Join(user.HomeDir, ".tradebot")
	value := os.Getenv("TRADEBOT_DATA_DIR")
	if len(value) == 0 {
		return defaultValue
	}

	if !filepath.IsAbs(value) {
		log.Printf("TRADEBOT_DATA_DIR value must be an absolute path (value %s is ignored)", value)
		return defaultValue
	}
	return value
}

func LogDir() string {
	var dataDir = DataDir()

	var defaultValue = dataDir
	value := os.ExpandEnv(os.Getenv("TRADEBOT_LOG_DIR"))
	if len(value) == 0 {
		return defaultValue
	}

	if !filepath.IsAbs(value) {
		if strings.ContainsRune(value, os.PathSeparator) {
			log.Printf("TRADEBOT_LOG_DIR value must be an absolute path or a directory base name (value %s is ignored)", value)
			return defaultValue
		}
		return filepath.Join(dataDir, value)
	}
	return value
}
