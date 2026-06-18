#!/bin/bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login -H "Content-Type: application/json" -d '{"email":"k6loadtest@example.com", "password":"K6LoadTest!2026"}' | jq -r .data.access_token)
if [ "$TOKEN" == "null" ] || [ -z "$TOKEN" ]; then
    echo "Failed to login."
    exit 1
fi

echo "Got token. Fetching projects..."
PROJECTS=$(curl -s -X GET http://localhost:8080/api/v1/projects -H "Authorization: Bearer $TOKEN" | jq -r '.data[].id')

for id in $PROJECTS; do
    echo "Deleting project $id..."
    curl -s -X DELETE "http://localhost:8080/api/v1/projects/$id" -H "Authorization: Bearer $TOKEN"
    echo ""
done
echo "Cleanup complete."
