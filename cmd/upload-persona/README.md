# Upload Persona and Knowledge Base to MongoDB

This script uploads the Clear Perceptions AI Assistant persona and knowledge base to MongoDB.

## Usage

From the `backend` directory, run:

```bash
make upload-persona
```

Or directly:

```bash
go run ./cmd/upload-persona
```

## What it does

1. **Reads the persona file** (`Clear_Perceptions_AI_Assistant_Persona.txt`) from the project root
2. **Creates or updates a persona** in MongoDB with:
   - Name: "Clear Perceptions AI Assistant"
   - Description: "AI Assistant for SAT, GRE, GMAT coaching & counselling"
   - Tone: "Warm, clear, structured, student-friendly. Professional, encouraging, and academically supportive."
   - Instructions: Full content from the persona file

3. **Reads the knowledge base file** (`Clear_Perceptions_Knowledge_Base.txt`) from the project root
4. **Saves the knowledge base file** to `backend/uploads/documents/`
5. **Creates or updates a document** in MongoDB linked to the persona with:
   - Name: "Clear Perceptions Knowledge Base"
   - Type: "knowledge_base"
   - Linked to the persona via `persona_id`

## Requirements

- MongoDB must be running and accessible
- `.env` file must be configured with `MONGO_URI` and `DB_NAME`
- The persona and knowledge base text files must be in the project root directory

## Output

The script will display:
- ✅ Success messages for each step
- ⚠️ Warnings if persona/document already exists (will update instead)
- The Persona ID that you can use in campaigns

## Using the Persona

After uploading, you can use this persona in your campaigns by setting the `persona_id` field to the ID returned by the script.

