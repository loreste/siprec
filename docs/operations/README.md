# Operations Documentation

Comprehensive operations guide for running SIPREC Server in production.

## Operations Overview

- [Production Deployment](PRODUCTION_DEPLOYMENT.md) - Complete production deployment guide
- [Production Readiness](PRODUCTION_READINESS.md) - Production readiness checklist
- [Migration Guide](MIGRATION.md) - Version migration procedures
- [Session Redundancy](SESSION_REDUNDANCY.md) - High availability configuration
- [Resource Optimization](RESOURCE_OPTIMIZATION.md) - Performance optimization guide
- [Monitoring & Alerting](MONITORING.md) - Operations monitoring setup
- [Backup & Recovery](BACKUP.md) - Data protection procedures

## Quick Operations Guide

### Health Checks

```bash
# Check service health
curl http://localhost:8080/health

# Check readiness
curl http://localhost:8080/ready

# Check liveness
curl http://localhost:8080/live
```

### Monitoring

```bash
# View metrics
curl http://localhost:8080/metrics

# Check active sessions
curl http://localhost:8080/api/sessions
```

### Logs

```bash
# View logs
tail -f /var/log/siprec/server.log

# Filter by level
grep "ERROR" /var/log/siprec/server.log
```

## Operations Checklist

### Daily Operations
- [ ] Check service health
- [ ] Review error logs
- [ ] Monitor resource usage
- [ ] Verify backups

### Weekly Operations
- [ ] Analyze performance metrics
- [ ] Review security logs
- [ ] Update dependencies
- [ ] Test failover procedures

### Monthly Operations
- [ ] Capacity planning review
- [ ] Security audit
- [ ] Performance optimization
- [ ] Documentation updates

## Support

For operational support:
- Check troubleshooting guides
- Review monitoring dashboards
- Contact operations team