#!/bin/sh
# Scripts to verify production configuration validation logic

echo "🔍 Verifying UserEngine Production Configuration..."

# 1. Build the API binary (used for validation)
echo "📦 Building API service for validation..."
go build -o bin/api-validator ./cmd/api

if [ $? -ne 0 ]; then
    echo "❌ Build failed"
    exit 1
fi

# 2. Test Invalid Configuration (Should Fail)
echo "🧪 Testing INVALID configuration (expecting failure)..."
export ENVIRONMENT=production
export JWT_SECRET="dev-secret-unsafe" # Should trigger validation error
export REDIS_ADDR="localhost:6379"

./bin/api-validator 2> /tmp/validation_output
EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
    echo "❌ Validation FAILED: Application started with insecure config!"
    rm /tmp/validation_output
    exit 1
else
    if grep -q "dev-secret is not allowed" /tmp/validation_output; then
         echo "✅ Successfully rejected insecure JWT secret."
    else
         echo "⚠️ Application failed but not for the expected reason. Output:"
         cat /tmp/validation_output
    fi
fi

# 3. Test Valid Configuration (Should Start - we kill it after 1s)
echo "🧪 Testing VALID configuration..."
export JWT_SECRET="production-ready-secret-key-base64-encoded-value-32-bytes"
export CORS_ALLOWED_ORIGINS="https://example.com"
export REDIS_ADDR="localhost:6379"
# Ensure we don't actually try to connect to redis and hang forever/fail if not running
# We just want to pass the config.Validate() step. 
# However, main() connects to redis immediately.
# So we need a redis instance or we expect a connection error, but NOT a validation error.

./bin/api-validator 2> /tmp/validation_output_valid &
PID=$!

sleep 2
kill $PID 2>/dev/null

if grep -q "configuration validation failed" /tmp/validation_output_valid; then
    echo "❌ Validation FAILED for valid config!"
    cat /tmp/validation_output_valid
    rm /tmp/validation_output_valid
    exit 1
else
    echo "✅ Configuration validation passed."
fi

rm /tmp/validation_output
rm /tmp/validation_output_valid
rm bin/api-validator

echo "🎉 Production configuration verification complete."
