# Security Policy

## Supported Versions

We actively support the following versions of ElasticRelay with security updates:

| Version | Supported          |
| ------- | ------------------ |
| 1.x.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security vulnerability in ElasticRelay, please report it responsibly:

### How to Report

**Please DO NOT file a public issue for security vulnerabilities.**

Instead, please send an email to: **security@yogoo.net**

Include the following information:
- Description of the vulnerability
- Steps to reproduce the issue
- Potential impact
- Any suggested fixes (if available)

### What to Expect

- **Acknowledgment**: We will acknowledge receipt of your vulnerability report within 48 hours.
- **Assessment**: We will assess the vulnerability and determine its severity within 5 business days.
- **Resolution**: We will work on a fix and coordinate the release of the security patch.
- **Credit**: We will publicly acknowledge your contribution (unless you prefer to remain anonymous).

### Security Best Practices

When deploying ElasticRelay:

1. **Secure Configuration**:
   - Use strong passwords for database connections
   - Enable TLS/SSL for Elasticsearch connections
   - Restrict network access using firewalls

2. **Access Control**:
   - Run ElasticRelay with minimal required privileges
   - Use dedicated database users with limited permissions
   - Regularly rotate credentials

3. **Monitoring**:
   - Enable logging and monitor for unusual activity
   - Set up alerts for failed authentication attempts
   - Regularly review access logs

4. **Updates**:
   - Keep ElasticRelay updated to the latest version
   - Subscribe to security announcements
   - Test updates in a non-production environment first

## Vulnerability Disclosure Timeline

We aim to disclose vulnerabilities according to the following timeline:

- **Day 0**: Vulnerability reported
- **Day 1-2**: Acknowledgment sent to reporter
- **Day 3-7**: Vulnerability assessed and confirmed
- **Day 8-30**: Fix developed and tested
- **Day 31**: Security patch released
- **Day 38**: Public disclosure (7 days after patch release)

This timeline may be adjusted based on the complexity and severity of the vulnerability.

## Contact

For any security-related questions or concerns, please contact:
- Email: security@yogoo.net
- For general questions: support@yogoo.net
