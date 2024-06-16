package config

import (
	"io"
	"log"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	NameOfDevice string `yaml:"name"`
	Functions    struct {
		HttpFileServer  bool `yaml:"http_file_server"`
		LocalSendServer bool `yaml:"local_send_server"`
	} `yaml:"functions"`
}

var ConfigData Config

func init() {
	configFile, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
	}
	defer configFile.Close()

	bytes, err := io.ReadAll(configFile)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	err = yaml.Unmarshal(bytes, &ConfigData)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}
}
