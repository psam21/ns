# Port Mapping for Multi-Environment Setup

This document outlines the port assignments for running development, testing, and production environments simultaneously on the same host.

## Port Assignments

### Development Environment
- **WebSocket**: `8081` (WS_ADDR)
- **Metrics**: `8182` (METRICS.PORT)
- **Database**: `5433` (PostgreSQL)

### Testing Environment
- **WebSocket**: `8082` (WS_ADDR)
- **Metrics**: `8183` (METRICS.PORT)
- **Database**: `5434` (PostgreSQL)

### Production Environment
- **WebSocket**: `8080` (WS_ADDR)
- **Metrics**: `8180` (METRICS.PORT)
- **Database**: `5432` (PostgreSQL)

## Configuration Files

- **Development**: `config/development.yaml`
- **Testing**: `config/test.yaml`
- **Production**: `config/production.yaml`

## Docker Compose Files

- **Development DB**: `docker/compose/docker-compose.development.yml`
- **Testing DB**: `docker/compose/docker-compose.test.yml`
- **Production**: `docker/compose/docker-compose.standalone.yml`

## Usage Examples

### Start Development Environment
```bash
# Start development database
docker-compose -f docker/compose/docker-compose.development.yml up -d

# Run relay with development config
./relay --config config/development.yaml
```

### Start Testing Environment
```bash
# Start testing database
docker-compose -f docker/compose/docker-compose.test.yml up -d

# Run relay with test config
./relay --config config/test.yaml
```

### Start Production Environment
```bash
# Start production database
docker-compose -f docker/compose/docker-compose.standalone.yml up -d

# Run relay with production config
./relay --config config/production.yaml
```

## Access URLs

### Development
- **WebSocket**: `ws://localhost:8081`
- **Metrics**: `http://localhost:8182/metrics`

### Testing
- **WebSocket**: `ws://localhost:8082`
- **Metrics**: `http://localhost:8183/metrics`

### Production
- **WebSocket**: `ws://localhost:8080`
- **Metrics**: `http://localhost:8181/metrics`

## Port Conflict Prevention

All ports are carefully assigned to avoid conflicts:
- WebSocket ports: 8080, 8081, 8082
- Metrics ports: 8180, 8181, 8182, 8183
- Database ports: 5432, 5433, 5434

## Environment Variables

You can override ports using environment variables:

```bash
# Development
export SHUGUR_WS_ADDR=":8081"
export SHUGUR_METRICS_PORT="8182"
export SHUGUR_DB_PORT="5433"

# Testing
export SHUGUR_WS_ADDR=":8082"
export SHUGUR_METRICS_PORT="8183"
export SHUGUR_DB_PORT="5434"

# Production
export SHUGUR_WS_ADDR=":8080"
export SHUGUR_METRICS_PORT="8181"
export SHUGUR_DB_PORT="5432"
```
