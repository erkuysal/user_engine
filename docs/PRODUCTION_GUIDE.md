# UserEngine Production Deployment Guide

This guide details how to configure and deploy UserEngine in a production environment.

## 1. Configuration Check

UserEngine enforces strict validation when `ENVIRONMENT=production` is set. The application will **fail to start** if security settings are configured insecurely.

### Required Environment Variables

| Variable | Description | Requirement in Production |
|----------|-------------|---------------------------|
| `ENVIRONMENT` | Run mode | Must be set to `production` |
| `JWT_SECRET` | Token signing key | **Required**. Cannot start with `dev-secret`. |
| `CORS_ALLOWED_ORIGINS` | Allowed browser origins | **Required**. Cannot be empty. Wildcards (`*`) not supported via config boolean. |
| `CORS_ALLOW_ALL` | Allow all origins | **Must be false** (default). |
| `WEBHOOK_SECRET` | Webhook signature key | **Required** if `WEBHOOK_ENABLED=true`. |

### Integration with Backend

Ensure these values match your Main Backend configuration:

- `JWT_SECRET` in UserEngine must match `USERENGINE_PRESENCE_JWT_SECRET` in Django Backend.
- `WEBHOOK_SECRET` in UserEngine must match `USERENGINE_WEBHOOK_SECRET` in Django Backend.

## 2. Docker Deployment

### Docker Compose Example

```yaml
version: '3.8'
services:
  userengine-gateway:
    image: ghcr.io/erkuysal/userengine:latest
    command: /app/service
    environment:
      - SERVICE=gateway
      - ENVIRONMENT=production
      - REDIS_ADDR=redis:6379
      - JWT_SECRET=${USERENGINE_PRESENCE_JWT_SECRET}
      - CORS_ALLOWED_ORIGINS=https://yourdomain.com
    ports:
      - "8080:8080"
    depends_on:
      - redis

  userengine-api:
    image: ghcr.io/erkuysal/userengine:latest
    command: /app/service
    environment:
      - SERVICE=api
      - ENVIRONMENT=production
      - REDIS_ADDR=redis:6379
      - JWT_SECRET=${USERENGINE_PRESENCE_JWT_SECRET}
      - WEBHOOK_ENABLED=true
      - WEBHOOK_SECRET=${USERENGINE_WEBHOOK_SECRET}
    ports:
      - "8081:8081"
    depends_on:
      - redis
```

## 3. Verification

Before deploying, you can run the verification script to ensure your configuration overrides pass validation:

```bash
# Validates production settings without starting the full service
./scripts/verify-prod-config.sh 
```
