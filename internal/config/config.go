package config

import (
	"embed"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"gopkg.in/yaml.v2"
)

//go:embed config.yaml
var embeddedConfig embed.FS

type Config struct {
	NameOfDevice string
	Functions    struct {
		HttpFileServer  bool `yaml:"http_file_server"`
		LocalSendServer bool `yaml:"local_send_server"`
	} `yaml:"functions"`
}

// random device name
var (
	adjectives = []string{
		"Happy", "Swift", "Silent", "Clever", "Brave",
		"Gentle", "Wise", "Calm", "Lucky", "Proud",
	}
	nouns = []string{
		"Phoenix", "Wolf", "Eagle", "Lion", "Owl",
		"Shark", "Tiger", "Bear", "Hawk", "Fox",
	}
)

var ConfigData Config

// random device name generator
func generateRandomName() string {
	localRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	adj := adjectives[localRand.Intn(len(adjectives))]
	noun := nouns[localRand.Intn(len(nouns))]
	return fmt.Sprintf("%s %s", adj, noun)
}

func init() {
	bytes, err := os.ReadFile("internal/config/config.yaml")
	if err != nil {
		log.Println("读取外部配置文件失败，使用内置配置")
		bytes, err = embeddedConfig.ReadFile("config.yaml")
		if err != nil {
			log.Fatalf("无法读取嵌入式配置文件: %v", err)
		}
	}

	if err := yaml.Unmarshal(bytes, &ConfigData); err != nil {
		log.Fatalf("解析配置文件出错: %v", err)
	}

	ConfigData.NameOfDevice = generateRandomName()
}
