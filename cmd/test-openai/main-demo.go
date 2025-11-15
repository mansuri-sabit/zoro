package main

import (
	"fmt"
	"os"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	
	fmt.Println("🔍 OpenAI API Key Check")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	
	if apiKey == "" {
		fmt.Println("❌ ERROR: OPENAI_API_KEY environment variable not set")
		fmt.Println()
		fmt.Println("📝 To set it, use one of these methods:")
		fmt.Println()
		fmt.Println("   Windows PowerShell:")
		fmt.Println("   $env:OPENAI_API_KEY='sk-your-key-here'")
		fmt.Println()
		fmt.Println("   Windows CMD:")
		fmt.Println("   set OPENAI_API_KEY=sk-your-key-here")
		fmt.Println()
		fmt.Println("   Linux/Mac:")
		fmt.Println("   export OPENAI_API_KEY=sk-your-key-here")
		fmt.Println()
		fmt.Println("   Then run:")
		fmt.Println("   go run cmd/test-openai/main.go")
		fmt.Println()
		os.Exit(1)
	}
	
	fmt.Printf("✅ OPENAI_API_KEY found!\n")
	fmt.Printf("   Length: %d characters\n", len(apiKey))
	
	if len(apiKey) >= 3 && apiKey[:3] == "sk-" {
		fmt.Println("   ✅ Format: Correct (starts with 'sk-')")
	} else {
		fmt.Println("   ⚠️  Format: Doesn't start with 'sk-'")
	}
	
	fmt.Println()
	fmt.Println("🔍 To test the API, run:")
	fmt.Println("   go run cmd/test-openai/main.go")
	fmt.Println()
}

