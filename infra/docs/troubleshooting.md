# Troubleshooting Guide

Common issues and solutions for OpenMentor infrastructure.

## Table of Contents

1. [Container Issues](#container-issues)
2. [Network Issues](#network-issues)
3. [SSL/TLS Certificate Issues](#ssltls-certificate-issues)
4. [Performance Issues](#performance-issues)
5. [Database/External API Issues](#databaseexternal-api-issues)
6. [Monitoring & Logging Issues](#monitoring--logging-issues)
7. [Deployment Issues](#deployment-issues)
8. [Emergency Procedures](#emergency-procedures)

---

## Container Issues

### Container Won't Start

**Symptoms:**
- Container status shows "Restarting" or "Exited"
- `docker-compose ps` shows unhealthy containers

**Diagnosis:**
```bash
# Check logs
docker-compose logs <service-name>

# Check container details
docker inspect <container-name>

# Check exit code
docker-compose ps
```

**Common Causes & Solutions:**

1. **Missing Environment Variables**
   ```bash
   # Check env vars
   docker exec -it <container> env

   # Solution: Update .env file
   nano /opt/openmentor/infra/.env
   docker-compose up -d
   ```

2. **Port Already in Use**
   ```bash
   # Check what's using the port
   sudo lsof -i :80
   sudo lsof -i :443

   # Solution: Stop conflicting service or change port
   sudo systemctl stop nginx  # If you have nginx running
   ```

3. **Out of Memory**
   ```bash
   # Check memory usage
   free -h
   docker stats

   # Solution: Increase VM RAM or restart services
   docker-compose restart
   ```

4. **Image Pull Failure**
   ```bash
   # Re-authenticate against AWS ECR (D19). On the VM, use the
   # openmentor-vm pull credentials from /opt/openmentor/infra/.env
   # from your LOCAL machine (the VM holds no AWS credentials):
   aws ecr get-login-password --region eu-central-1 | \
     ssh deploy@<vm> "docker login --username AWS --password-stdin '<ECR_REGISTRY>'"
   aws ecr get-login-password --region $(grep '^AWS_REGION=' .env | cut -d'=' -f2) \
     | docker login --username AWS --password-stdin $(grep '^ECR_REGISTRY=' .env | cut -d'=' -f2)
   unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY

   # Retry pull
   docker-compose pull
   docker-compose up -d
   ```

### Container Keeps Restarting

**Symptoms:**
- Container in restart loop
- Restart count increasing

**Diagnosis:**
```bash
# Watch container status
watch -n 1 'docker-compose ps'

# Check last 50 log lines
docker-compose logs --tail=50 <service>

# Check restart count
docker inspect <container> | grep -A 5 RestartCount
```

**Solutions:**

1. **Application Crash on Startup**
   ```bash
   # Check logs for panic/error
   docker-compose logs backend | grep -i "panic\|error\|fatal"

   # Solution: Fix configuration or code issue
   # Rollback to previous version if needed (./rollback.sh <previous-sha>
   # from a workstation, or manually on the VM):
   sed -i "s/^BACKEND_IMAGE_TAG=.*/BACKEND_IMAGE_TAG=<previous-sha>/" .env  # and/or FRONTEND_IMAGE_TAG
   docker-compose pull
   docker-compose up -d
   ```

2. **Health Check Failing**
   ```bash
   # Test health check manually
   docker exec -it <container> curl http://localhost:8081/api/healthcheck

   # Disable health check temporarily
   # Edit docker-compose.yml, comment out healthcheck
   docker-compose up -d
   ```

3. **Dependency Not Ready**
   ```bash
   # Backend waiting for database
   # Check depends_on and health checks

   # Solution: Ensure dependencies start first
   docker-compose up -d backend
   sleep 10
   docker-compose up -d frontend
   ```

---

## Network Issues

### Frontend Can't Reach Backend

**Symptoms:**
- Frontend shows "API Error" or "Connection Refused"
- 502 Bad Gateway errors

**Diagnosis:**
```bash
# Check if backend is running
docker-compose ps backend

# Test backend from frontend container
docker exec -it openmentor-frontend curl http://backend:8081/api/healthcheck

# Check Docker network
docker network inspect openmentor_openmentor-network
```

**Solutions:**

1. **Wrong Service Name**
   ```bash
   # Verify NEXT_PUBLIC_GO_API_URL
   docker exec -it openmentor-frontend env | grep GO_API

   # Should be: http://backend:8081
   # NOT: http://localhost:8081

   # Solution: Update .env
   NEXT_PUBLIC_GO_API_URL=http://backend:8081
   docker-compose up -d frontend
   ```

2. **Backend Not Listening on 0.0.0.0**
   ```bash
   # Check backend binding
   docker exec -it openmentor-backend netstat -tlnp | grep 8081

   # Should show: 0.0.0.0:8081
   # NOT: 127.0.0.1:8081

   # Solution: Update backend code to bind to 0.0.0.0
   ```

3. **Network Not Created**
   ```bash
   # List networks
   docker network ls

   # Create network manually
   docker network create openmentor_openmentor-network
   docker-compose up -d
   ```

### External Services Unreachable

**Symptoms:**
- Can't connect to PostgreSQL
- Can't reach S3 object storage
- Can't resolve DNS

**Diagnosis:**
```bash
# Test DNS resolution from container
docker exec -it openmentor-backend nslookup google.com

# Test PostgreSQL connection
docker exec -it openmentor-backend psql $DATABASE_URL -c "SELECT 1"

# Check firewall rules
sudo ufw status
```

**Solutions:**

1. **Firewall Blocking Outbound**
   ```bash
   # Allow outbound traffic
   sudo ufw default allow outgoing
   ```

2. **DNS Issues**
   ```bash
   # Update DNS servers in Docker daemon
   sudo nano /etc/docker/daemon.json

   {
     "dns": ["8.8.8.8", "8.8.4.4"]
   }

   sudo systemctl restart docker
   docker-compose up -d
   ```

---

## SSL/TLS Certificate Issues

### Let's Encrypt Certificate Not Generated

**Symptoms:**
- HTTPS shows "Connection not secure"
- Browser certificate error
- Traefik logs show ACME errors

**Diagnosis:**
```bash
# Check Traefik logs
docker-compose logs traefik | grep -i acme

# Check certificate storage
sudo ls -la /var/lib/docker/volumes/traefik-letsencrypt-certificates/_data/

# Test HTTP-01 challenge
curl http://openmentor.io/.well-known/acme-challenge/test
```

**Solutions:**

1. **DNS Not Pointing to Server**
   ```bash
   # Verify DNS
   dig openmentor.io
   nslookup openmentor.io

   # Solution: Update DNS A record to VM IP
   # Wait 15-30 minutes for propagation
   ```

2. **Port 80 Not Accessible**
   ```bash
   # Check firewall
   sudo ufw status | grep 80

   # Test port 80 externally
   curl http://openmentor.io

   # Solution: Open port 80
   sudo ufw allow 80/tcp
   ```

3. **Rate Limit Hit**
   ```bash
   # Let's Encrypt rate limits:
   # - 50 certificates per domain per week
   # - 5 failed validations per hour

   # Solution: Use staging ACME server
   # Uncomment in docker-compose.yml:
   # --certificatesresolvers.letsencrypt.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory

   docker-compose up -d traefik
   ```

4. **Invalid Email**
   ```bash
   # Check LETSENCRYPT_EMAIL in .env
   # Must be valid email address

   # Update .env
   LETSENCRYPT_EMAIL=admin@openmentor.io
   docker-compose up -d traefik
   ```

### Certificate Expired

**Symptoms:**
- Certificate expiry warning
- HTTPS stops working after 90 days

**Diagnosis:**
```bash
# Check certificate expiry
echo | openssl s_client -servername openmentor.io -connect openmentor.io:443 2>/dev/null | openssl x509 -noout -dates
```

**Solutions:**

```bash
# Traefik should auto-renew
# If not, force renewal:

# 1. Delete old certificate
docker-compose down
sudo rm -rf /var/lib/docker/volumes/traefik-letsencrypt-certificates/_data/acme.json
docker-compose up -d

# 2. Check Traefik logs
docker-compose logs traefik | grep -i renew
```

---

## Performance Issues

### High CPU Usage

**Diagnosis:**
```bash
# Check container CPU
docker stats

# Check VM CPU
top
htop

# Check per-process
docker exec -it openmentor-backend top
```

**Solutions:**

1. **Infinite Loop or Busy Wait**
   ```bash
   # Check application logs
   docker-compose logs backend | tail -100

   # Profile the application
   # For Go: enable pprof, analyze CPU profile
   # For Node: use clinic.js

   # Solution: Fix code issue, restart
   docker-compose restart backend
   ```

2. **Too Many Requests**
   ```bash
   # Check request rate in Grafana
   # Look for spike in http_server_request_total

   # Solution: Increase rate limits or scale up
   # Edit rate limits in backend code
   # Or increase VM CPU
   ```

### High Memory Usage

**Diagnosis:**
```bash
# Check container memory
docker stats

# Check memory breakdown
docker exec -it openmentor-backend free -h

# Check for memory leaks
# Monitor over time, look for gradual increase
```

**Solutions:**

1. **Memory Leak**
   ```bash
   # Restart affected service
   docker-compose restart backend

   # Monitor for recurrence
   # If repeats, investigate code for leaks
   ```

2. **Cache Too Large**
   ```bash
   # Check cache size in application
   # Backend has in-memory cache with TTL

   # Solution: Reduce cache size or TTL
   # Or increase VM memory
   ```

3. **Increase Container Memory Limit**
   ```yaml
   # In docker-compose.yml
   services:
     backend:
       mem_limit: 1g
       mem_reservation: 512m
   ```

### Slow Response Times

**Diagnosis:**
```bash
# Test endpoint latency
time curl https://openmentor.io/api/mentors

# Check backend logs
docker-compose logs backend | grep -i duration

# Check metrics in Grafana
# Query: http_server_request_duration_seconds
```

**Solutions:**

1. **External API Slow**
   ```bash
   # Check PostgreSQL query time
   # Check S3 object storage latency

   # Solution: Enable/tune caching
   # Backend already has 60s TTL cache
   ```

2. **Database/Network Latency**
   ```bash
   # Test PostgreSQL connection latency
   docker exec -it openmentor-backend psql $DATABASE_URL -c "SELECT 1"

   # Solution: Optimize database queries
   # Or increase cache TTL
   ```

3. **Not Enough Resources**
   ```bash
   # Check if CPU/memory maxed
   docker stats

   # Solution: Scale up VM
   # Or optimize code
   ```

---

## Database/External API Issues

### PostgreSQL Connection Errors

**Symptoms:**
- "Failed to fetch mentors" errors
- "Database connection failed" errors
- "Connection refused" errors

**Diagnosis:**
```bash
# Check backend logs
docker-compose logs backend | grep -i postgres

# Test database connection
docker exec -it openmentor-backend psql $DATABASE_URL -c "SELECT COUNT(*) FROM mentors"
```

**Solutions:**

1. **Invalid Connection String**
   ```bash
   # Check DATABASE_URL in .env
   cat /opt/openmentor/infra/.env | grep DATABASE_URL

   # Solution: Update with valid connection string
   # Format: postgres://user:password@host:5432/database
   nano /opt/openmentor/infra/.env
   docker-compose up -d backend
   ```

2. **Connection Pool Exhausted**
   ```bash
   # Check connection pool settings
   docker-compose logs backend | grep "connection pool"

   # Solution: Increase pool size or reduce concurrent queries
   # Or add connection pooling with PgBouncer
   ```

3. **Database Not Accessible**
   ```bash
   # Verify PostgreSQL is running
   # Check DATABASE_URL in .env

   # Test connection from VM
   psql $DATABASE_URL -c "SELECT 1"

   # Check firewall rules if external DB
   ```

### S3 Object Storage Errors

**Symptoms:**
- Image upload fails
- "Failed to save profile picture"

**Diagnosis:**
```bash
# Check backend logs
docker-compose logs backend | grep -i storage

# Test connection
docker exec -it openmentor-backend curl -I $S3_STORAGE_ENDPOINT
```

**Solutions:**

1. **Invalid Credentials or Endpoint**
   ```bash
   # Check S3_STORAGE_ACCESS_KEY / S3_STORAGE_SECRET_KEY /
   # S3_STORAGE_ENDPOINT / S3_STORAGE_REGION in .env

   # Solution: Get new credentials from your storage provider (R2/S3/B2)
   nano /opt/openmentor/infra/.env
   docker-compose up -d backend
   ```

2. **Bucket Not Found**
   ```bash
   # Verify bucket name
   # Check S3_STORAGE_BUCKET in .env

   # Solution: Create the bucket at your storage provider
   # Or update the bucket name in .env
   ```

---

## Monitoring & Logging Issues

### Grafana Cloud Not Receiving Data

**Symptoms:**
- No metrics in Grafana dashboards
- Alloy container running but no data

**Diagnosis:**
```bash
# Check Alloy logs
docker-compose logs alloy | grep -i error

# Check Alloy metrics endpoint
curl http://localhost:12345/metrics | grep alloy_

# Test remote write
docker-compose logs alloy | grep "remote_write"
```

**Solutions:**

1. **Invalid Grafana Cloud Credentials**
   ```bash
   # Check credentials in .env
   cat /opt/openmentor/infra/.env | grep GCLOUD_

   # Solution: Update with valid credentials
   # Get from: Grafana Cloud → Configuration → Data Sources
   ```

2. **Network Issues**
   ```bash
   # Test connectivity
   docker exec -it grafana-alloy curl -I https://prometheus-us-central1.grafana.net

   # Check firewall
   sudo ufw status
   ```

3. **Alloy Config Error**
   ```bash
   # Validate Alloy config
   docker run --rm -v $(pwd)/alloy:/etc/alloy grafana/alloy:latest \
     run --config.file=/etc/alloy/config.alloy --dry-run

   # Fix syntax errors in alloy/config.alloy
   ```

### Logs Not Appearing

**Symptoms:**
- Empty logs in Grafana Loki
- Application logs not being shipped

**Diagnosis:**
```bash
# Check if logs are being written
docker-compose logs backend --tail=50

# Check Alloy log collection
docker-compose logs alloy | grep loki
```

**Solutions:**

1. **Log Files Not Created**
   ```bash
   # Check log directory
   docker exec -it openmentor-backend ls -la /app/logs/

   # Solution: Ensure LOG_DIR is writable
   docker exec -it openmentor-backend chmod 777 /app/logs
   ```

2. **Alloy Not Tailing Logs**
   ```bash
   # Alloy config for log files is commented out by default
   # (Logs are shipped via HTTP transport instead)

   # If you want file tailing, uncomment sections in alloy/config.alloy
   # And mount backend logs to Alloy container
   ```

---

## Deployment Issues

### Deployment Hangs

**Symptoms:**
- `docker-compose up -d` never completes
- Services stuck in "Starting" state

**Diagnosis:**
```bash
# Check what's happening
docker-compose ps
docker-compose logs --tail=50

# Check disk space
df -h

# Check Docker daemon
sudo systemctl status docker
```

**Solutions:**

1. **Out of Disk Space**
   ```bash
   # Clean up Docker
   docker system prune -a --volumes

   # Remove old images
   docker image prune -a

   # Check space
   df -h
   ```

2. **Corrupted Image**
   ```bash
   # Remove image and re-pull (registry host = ECR_REGISTRY from .env)
   docker rmi ${ECR_REGISTRY}/openmentor-frontend:<tag>
   docker-compose pull
   docker-compose up -d
   ```

3. **Docker Daemon Stuck**
   ```bash
   # Restart Docker
   sudo systemctl restart docker

   # Wait 30 seconds
   sleep 30

   # Retry
   docker-compose up -d
   ```

### Health Checks Never Pass

**Symptoms:**
- Container stays in "starting" state
- Health: starting (health: starting)

**Diagnosis:**
```bash
# Check health check endpoint
docker exec -it openmentor-frontend curl -v http://localhost:3000/api/healthcheck

# Check container logs
docker-compose logs frontend
```

**Solutions:**

1. **Application Not Ready**
   ```bash
   # Increase start_period in health check
   # Edit docker-compose.yml:
   healthcheck:
     start_period: 60s  # Increase from 40s

   docker-compose up -d
   ```

2. **Health Endpoint Not Implemented**
   ```bash
   # Verify endpoint exists
   curl https://openmentor.io/api/healthcheck

   # If missing, implement or disable health check
   ```

---

## Emergency Procedures

### Complete Site Down

**Immediate Actions:**

1. **Check if VM is running**
   ```bash
   # From local machine
   ping ${VM_IP}
   ssh -i /path/to/ssh-key <user>@${VM_IP}
   ```

2. **Check Docker containers**
   ```bash
   docker-compose ps
   docker-compose logs --tail=100
   ```

3. **Quick restart all services**
   ```bash
   docker-compose restart
   ```

4. **If restart fails, full reset**
   ```bash
   docker-compose down
   docker-compose up -d
   ```

5. **If still down, rollback**
   ```bash
   # ./rollback.sh <last-known-good-sha> from a workstation, or on the VM:
   sed -i "s/^BACKEND_IMAGE_TAG=.*/BACKEND_IMAGE_TAG=<last-known-good-sha>/" .env  # and/or FRONTEND_IMAGE_TAG
   docker-compose pull
   docker-compose up -d
   ```

### Data Loss Prevention

**If you suspect data corruption:**

```bash
# 1. Stop writes immediately
docker-compose stop frontend backend

# 2. Backup current state
sudo cp -r /opt/openmentor/infra ~/backup-$(date +%Y%m%d-%H%M%S)

# 3. Investigate
# Check PostgreSQL (primary data source)
# Check the S3 bucket (images)

# 4. Restore from backup if needed
# (PostgreSQL backups via pg_dump)
# (enable S3 bucket versioning for image recovery)
```

### Security Incident

**If you suspect a breach:**

```bash
# 1. Isolate the system
sudo ufw deny in on eth0

# 2. Capture evidence
docker-compose logs > /tmp/incident-logs-$(date +%Y%m%d-%H%M%S).txt
sudo journalctl > /tmp/system-logs-$(date +%Y%m%d-%H%M%S).txt

# 3. Shut down compromised services
docker-compose down

# 4. Rotate all secrets
# Update all tokens in .env
# Rotate PostgreSQL credentials
# Rotate S3 and SES access keys
# Regenerate all authentication tokens

# 5. Rebuild from clean images
docker-compose pull
docker-compose up -d

# 6. Re-enable firewall properly
sudo ufw default deny incoming
sudo ufw allow 22,80,443/tcp
sudo ufw enable
```

---

## Getting Help

### Information to Gather

Before asking for help, collect:

```bash
# 1. Service status
docker-compose ps > debug-info.txt

# 2. Logs
docker-compose logs --tail=200 >> debug-info.txt

# 3. Environment
docker exec -it openmentor-backend env | grep -v SECRET >> debug-info.txt

# 4. System info
df -h >> debug-info.txt
free -h >> debug-info.txt
docker version >> debug-info.txt

# 5. Network
docker network inspect openmentor_openmentor-network >> debug-info.txt

# Send debug-info.txt to support
```

### Support Contacts

- **General Issues**: support@openmentor.io
- **Infrastructure**: devops@openmentor.io
- **Security**: security@openmentor.io

### Useful Resources

- [Docker Compose Documentation](https://docs.docker.com/compose/)
- [Traefik Documentation](https://doc.traefik.io/traefik/)
- [Grafana Alloy Documentation](https://grafana.com/docs/alloy/)
- [Hetzner Cloud Documentation](https://docs.hetzner.com/cloud/)

---

**Remember: When in doubt, check the logs first!** 📝
