# Contributing to ElasticRelay

Thank you for your interest in contributing to ElasticRelay! We welcome contributions from the community and are grateful for your support.

## 🚀 Getting Started

### Prerequisites

- **Go 1.25+**: Make sure you have Go installed
- **MySQL 8.0+**: For testing database connectivity
- **Elasticsearch 7.x/8.x**: For testing data synchronization
- **Docker & Docker Compose**: For local development environment

### Development Setup

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/yogoosoft/elasticrelay.git
   cd elasticrelay
   ```
3. **Set up development environment**:
   ```bash
   # Install dependencies
   go mod tidy
   
   # Start local services (MySQL + Elasticsearch)
   docker-compose up -d
   
   # Initialize test database
   mysql -h 127.0.0.1 -P 3306 -u root -p < init.sql
   ```
4. **Run tests**:
   ```bash
   make test
   ```

## 📝 How to Contribute

### Reporting Issues

Before creating an issue, please:
- **Search existing issues** to avoid duplicates
- **Use issue templates** when available
- **Provide detailed information**:
  - ElasticRelay version
  - Operating system and version
  - Go version
  - MySQL/Elasticsearch versions
  - Complete error messages and logs

### Submitting Pull Requests

1. **Create a new branch** for your feature/fix:
   ```bash
   git checkout -b feature/your-feature-name
   # or
   git checkout -b fix/issue-number
   ```

2. **Make your changes** following our coding standards:
   - Write clear, documented code
   - Add tests for new functionality
   - Ensure all tests pass
   - Follow Go conventions and best practices

3. **Commit your changes**:
   ```bash
   git add .
   git commit -m "feat: add support for PostgreSQL connector"
   # or
   git commit -m "fix: resolve DLQ timeout issue #123"
   ```

4. **Push to your fork**:
   ```bash
   git push origin feature/your-feature-name
   ```

5. **Create a Pull Request** with:
   - Clear title and description
   - Reference related issues
   - Include screenshots/demos if applicable

### Commit Message Format

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only changes
- `style`: Code style changes (formatting, missing semi-colons, etc)
- `refactor`: Code refactoring
- `test`: Adding missing tests
- `chore`: Changes to build process or auxiliary tools

Examples:
```
feat(connector): add PostgreSQL support
fix(dlq): resolve timeout issues in batch processing
docs: update API documentation for v1.3.0
```

## 🎯 Areas We Need Help

### High Priority
- **Multi-Database Connectors**: PostgreSQL, MongoDB, SQL Server
- **Performance Optimization**: Memory usage, throughput improvements
- **Documentation**: API docs, tutorials, examples
- **Testing**: Unit tests, integration tests, performance tests

### Good First Issues
Look for issues labeled [`good first issue`](https://github.com/yogoosoft/elasticrelay/labels/good%20first%20issue) - these are beginner-friendly tasks.

## 🧪 Testing Guidelines

### Running Tests
```bash
# Run all tests
make test

# Run specific test package
go test ./internal/connectors/mysql/...

# Run tests with coverage
make test-coverage

# Run integration tests
make test-integration
```

### Writing Tests
- **Unit Tests**: Test individual functions/methods
- **Integration Tests**: Test component interactions
- **End-to-End Tests**: Test complete workflows
- **Performance Tests**: Benchmark critical paths

Example test structure:
```go
func TestConnectorSync(t *testing.T) {
    // Arrange
    connector := NewMySQLConnector(testConfig)
    
    // Act
    err := connector.StartSync(context.Background())
    
    // Assert
    assert.NoError(t, err)
    assert.True(t, connector.IsConnected())
}
```

## 📖 Documentation

### Code Documentation
- **Public APIs**: Must have clear Go doc comments
- **Complex Logic**: Add inline comments explaining "why"
- **Examples**: Include usage examples in doc comments

### User Documentation
- **README**: Keep it updated and accurate
- **API Docs**: Document all public interfaces
- **Tutorials**: Step-by-step guides for common use cases

## 🌟 Recognition

Contributors are recognized in multiple ways:
- **Contributors Wall**: Your avatar appears in our README
- **Release Notes**: Major contributions are highlighted
- **Swag**: Core contributors receive ElasticRelay merchandise
- **Ambassador Program**: Opportunity to represent the project

## 📞 Getting Help

- 💬 **GitHub Discussions**: Best for questions and ideas
- 🐛 **GitHub Issues**: For bug reports and feature requests
- 📧 **Email**: dev@elasticrelay.io for sensitive matters
- 💻 **Discord**: [Join our developer community](https://discord.gg/elasticrelay)

## 📋 Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## 📄 License

By contributing to ElasticRelay, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).

---

Thank you for contributing to ElasticRelay! 🚀
