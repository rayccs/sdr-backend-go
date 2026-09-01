package main

import (
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type CompanyConfig struct {
	CompanyID     string `gorm:"primaryKey"`
	KnowledgeBase string
}

func main() {
	db, err := gorm.Open(sqlite.Open("sdr_backend.db"), &gorm.Config{})
	if err != nil {
		fmt.Println(err)
		return
	}
	var c CompanyConfig
	db.Where("company_id = ?", "rayccs-gmail-com").First(&c)
	fmt.Println("KNOWLEDGE BASE DUMP START---")
	fmt.Println(c.KnowledgeBase[len(c.KnowledgeBase)-500:]) // Only print last 500 chars to see the end
	fmt.Println("KNOWLEDGE BASE DUMP END---")
}
