# ElasticRelay - Multi-Source CDC Gateway zu Elasticsearch

![ElasticRelay Screenshot](/releases/download/asset/screenshot_02.png)

<p align="center">
  <a href="https://github.com/yogoosoft/ElasticRelay/releases"><img src="https://img.shields.io/badge/version-v1.3.1-blue.svg" alt="Version"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-1.25.2+-00ADD8.svg" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-green.svg" alt="Lizenz"></a>
</p>
<p align="center">
  <a href="/README.md">English</a> |
  <a href="README.de.md">Deutsch</a> |
  <a href="README.fr.md">Français</a> |
  <a href="README.ja.md">日本語</a> |
  <a href="README.ru.md">Русский</a> |
  <a href="README.zh-CN.md">中文</a>
</p>

## Vision

ElasticRelay ist ein nahtloser, heterogener Daten-Synchronisierer, der entwickelt wurde, um Echtzeit-Change Data Capture (CDC) von wichtigen OLTP-Datenbanken (MySQL, PostgreSQL, MongoDB) zu Elasticsearch bereitzustellen. Es zielt darauf ab, benutzerfreundlicher und zuverlässiger als bestehende Lösungen wie Logstash oder Flink zu sein.

## 🎉 v1.3.1 Highlights - Multi-Source CDC Plattform

**Drei Hauptdatenbankquellen vollständig unterstützt:**

| Quelle | Status | Funktionen |
|--------|--------|----------|
| **MySQL** | ✅ Vollständig | Binlog CDC + Initial Sync + Parallele Snapshots |
| **PostgreSQL** | ✅ Vollständig | Logische Replikation + WAL-Parsing + LSN-Verwaltung |
| **MongoDB** | ✅ Vollständig | Change Streams + Sharded Clusters + Resume Tokens |

## Hauptfunktionen

- **Multi-Source CDC**: Vollständige Unterstützung für MySQL, PostgreSQL und MongoDB mit Echtzeit-Änderungserfassung
- **Zero-Code Konfiguration**: JSON-basierte Konfiguration mit Assistenten-GUI (in Entwicklung)
- **Multi-Table Dynamische Indexierung**: Erstellt automatisch separate Elasticsearch-Indizes für jede Quelltabelle mit konfigurierbaren Namensmustern (z.B. `elasticrelay-users`, `elasticrelay-orders`)
- **Eingebaute Governance**: Handhabt Datenstrukturierung, Anonymisierung, Typkonvertierung, Normalisierung und Anreicherung
- **Zuverlässigkeit von Anfang an**: Nutzt CDC auf Transaktionslog-Ebene, präzises Checkpointing für Wiederaufnahme und idempotente Schreibvorgänge zur Sicherstellung der Datenintegrität
- **Dead Letter Queue (DLQ)**: Umfassende Fehlerbehandlung mit exponentiellem Backoff-Retry und persistentem Speicher
- **Parallele Verarbeitung**: Erweiterte parallele Snapshot-Verarbeitung mit Chunking-Strategien für große Tabellen

## Technologie-Stack

- **Data Plane (Go)**: Die Kern-Datensynchronisierungslogik ist in Go (1.25.2+) gebaut für hohe Nebenläufigkeit, geringen Speicherbedarf und einfache Bereitstellung.
- **Control Plane & GUI (TypeScript/Next.js)**: Eine reichhaltige, interaktive Benutzeroberfläche für Konfiguration und Überwachung (in Entwicklung).
- **APIs (gRPC)**: Interne Kommunikation zwischen Komponenten wird über gRPC für hohe Leistung mit vollständigen Service-Implementierungen abgewickelt.
- **Datenbankunterstützung**: 
  - **MySQL CDC**: Erweitertes Binlog-Parsing mit Echtzeit-Synchronisierung (go-mysql Bibliothek)
  - **PostgreSQL CDC**: Logische Replikation mit WAL-Parsing, Replikationsslots und Publications
  - **MongoDB CDC**: Change Streams mit Replica Set und Sharded Cluster Unterstützung (mongo-driver)
- **Elasticsearch Integration**: Offizieller Elasticsearch Go-Client (v8) mit Bulk-Indexierungsunterstützung
- **Konfiguration**: JSON-basierte Konfiguration mit automatischer Formaterkennung und Migration
- **Zuverlässigkeit**: Umfassende Fehlerbehandlung, DLQ-System und Checkpoint-Verwaltung

## Architektur

Das System besteht aus mehreren Schlüsselkomponenten:

- **Source Connectors**: Erfassen Änderungen aus Quelldatenbanken.
- **Durable Buffer**: Ein persistenter Puffer zur Entkopplung von Quellen und Senken und zur Ermöglichung von Replay-Fähigkeit.
- **Transform & Governance Engine**: Führt Datentransformationsregeln aus.
- **ES Sink Writer**: Schreibt Daten effizient in Batches nach Elasticsearch.
- **Orchestrator**: Verwaltet den Lebenszyklus von Synchronisierungsaufgaben.
- **Control Plane**: Die Benutzeroberfläche und das Konfigurationsmanagement-Backend.

## Schnellstart

Um ElasticRelay schnell zum Laufen zu bringen, folgen Sie diesen drei einfachen Schritten:

### Schritt 1: Bauen
```sh
./scripts/build.sh
```

### Schritt 2: Konfigurieren

#### MongoDB Setup (Erforderlich für MongoDB CDC)
MongoDB erfordert den Replica Set Modus für Change Streams. Führen Sie das Setup-Skript aus:
```sh
./scripts/reset-mongodb.sh
```

Oder manuell:
```sh
docker-compose down
rm -rf ./data/mongodb/*
docker-compose up -d mongodb
docker-compose up mongodb-init
```

Überprüfen Sie, ob MongoDB bereit ist:
```sh
./scripts/verify-mongodb.sh
```

📚 **Siehe**: `QUICKSTART.md` für detaillierte MongoDB-Setup-Anweisungen.

#### PostgreSQL Setup
Für PostgreSQL stellen Sie sicher, dass die logische Replikation aktiviert ist:
```sql
-- Logische Replikation in postgresql.conf aktivieren
wal_level = logical
max_replication_slots = 10
max_wal_senders = 10

-- Benutzer mit Replikationsrechten erstellen
CREATE USER elasticrelay_user WITH LOGIN PASSWORD 'password' REPLICATION;
GRANT CONNECT ON DATABASE your_database TO elasticrelay_user;
GRANT USAGE ON SCHEMA public TO elasticrelay_user;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO elasticrelay_user;
```

#### Konfigurationsdateien
Bearbeiten Sie die Konfigurationsdatei `./config/parallel_config.json` und stellen Sie sicher, dass die Datenbank- und Elasticsearch-Verbindungsinformationen korrekt sind.

### Schritt 3: Ausführen
```sh
./start.sh
```

Nach Abschluss dieser Schritte wird ElasticRelay beginnen, Datenbankänderungen zu überwachen und sie mit Elasticsearch zu synchronisieren.

---

## Ausführung

### Voraussetzungen

- Go (1.25.2+)
- Protobuf Compiler (`protoc`)
- Elasticsearch (7.x oder 8.x)
- **MySQL** (5.7+ oder 8.x) mit aktiviertem Binlog
- **PostgreSQL** (10+ empfohlen, 9.4+ Minimum) mit aktivierter logischer Replikation
- **MongoDB** (4.0+) mit Replica Set oder Sharded Cluster Konfiguration

### Installation

1.  **Go-Abhängigkeiten und Tools installieren**:
    ```sh
    go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
    ```

2.  **`protoc` installieren**:
    Auf macOS mit Homebrew:
    ```sh
    brew install protobuf
    ```

3.  **Abhängigkeiten aufräumen**:
    ```sh
    go mod tidy
    ```

### Server bauen und ausführen

#### Schnell-Build (Entwicklung)
```sh
# Einfacher Build ohne Versionsinformationen
go build -o elasticrelay ./cmd/elasticrelay

# Server ausführen
./elasticrelay -config multi_config.json
```

#### Produktions-Build (Empfohlen)
```sh
# Build mit Versionsinformationen über Makefile
make build

# Versionierte Binary ausführen
./bin/elasticrelay -config multi_config.json
```

#### Versionsverwaltung
ElasticRelay verfügt über umfassende Versionsverwaltung mit Build-Zeit-Injektion:

```sh
# Aktuelle Versionsinformationen mit detaillierten Build-Informationen anzeigen
./bin/elasticrelay -version

# Versionsinformationen vom Makefile prüfen
make version

# Entwicklungs-Build (schnell, ohne Versionsinjektion)
make dev

# Produktions-Build (optimiert mit Versionsinformationen)
make release

# Plattformübergreifende Builds für mehrere Architekturen
make build-all

# Build mit benutzerdefinierter Version
VERSION="v1.3.0" make build

# Alle Tools einschließlich Migrations-Utilities bauen
make build-tools
```

Das Versionssystem umfasst:
- **Git Integration**: Automatische Versionserkennung aus Git-Tags
- **Build-Metadaten**: Commit-Hash, Build-Zeit, Go-Version und Plattforminformationen
- **Farbige Ausgabe**: Reichhaltige Konsolenausgabe mit Versionsdetails und ASCII-Art-Logo
- **Plattformübergreifend**: Unterstützung für Linux, macOS (Intel/ARM) und Windows

Der Server wird standardmäßig auf Port `50051` starten und lauschen.

**Alternative**: Sie können auch direkt ohne Bauen ausführen:
```sh
go run ./cmd/elasticrelay -config multi_config.json
```

### Multi-Table Konfiguration

ElasticRelay unterstützt sowohl Legacy-Einzelkonfiguration als auch moderne Multi-Config-Formate mit automatischer Erkennung und Migration.

#### Modernes Multi-Config Format (`multi_config.json`):

```json
{
  "version": "3.0",
  "data_sources": [
    {
      "id": "mysql-main",
      "type": "mysql",
      "host": "localhost",
      "port": 3306,
      "user": "elastic_user",
      "password": "password",
      "database": "elasticrelay",
      "server_id": 100,
      "table_filters": ["users", "orders", "products"]
    },
    {
      "id": "postgresql-main",
      "type": "postgresql",
      "host": "localhost",
      "port": 5432,
      "user": "elastic_user",
      "password": "password",
      "database": "elasticrelay",
      "table_filters": ["users", "orders", "products"],
      "options": {
        "ssl_mode": "disable",
        "slot_name": "elasticrelay_slot",
        "publication_name": "elasticrelay_publication",
        "batch_size": 1000,
        "max_connections": 10,
        "parallel_snapshots": true
      }
    },
    {
      "id": "mongodb-main",
      "type": "mongodb",
      "host": "localhost",
      "port": 27017,
      "user": "elasticrelay_user",
      "password": "password",
      "database": "elasticrelay",
      "table_filters": ["users", "orders", "products"],
      "options": {
        "auth_source": "admin",
        "replica_set": "rs0"
      }
    }
  ],
  "sinks": [
    {
      "id": "es-main",
      "type": "elasticsearch",
      "addresses": ["http://localhost:9200"],
      "options": {
        "index_prefix": "elasticrelay"
      }
    }
  ],
  "jobs": [],
  "global": {
    "log_level": "info",
    "grpc_port": 50051,
    "dlq_config": {
      "enabled": true,
      "storage_path": "dlq",
      "max_retries": 3,
      "retry_delay": "30s"
    }
  }
}
```

#### Legacy-Konfigurationsformat (`config.json`):

```json
{
  "db_host": "localhost",
  "db_port": 3306,
  "db_user": "elastic_user",
  "db_password": "password",
  "db_name": "elasticrelay",
  "server_id": 100,
  "table_filters": ["users", "orders", "products"],
  "es_addresses": ["http://localhost:9200"]
}
```

Das System erkennt automatisch das Konfigurationsformat und unterstützt die Migration zwischen Formaten. Dies erstellt separate Indizes:
- `elasticrelay-users` für die `users`-Tabelle
- `elasticrelay-orders` für die `orders`-Tabelle  
- `elasticrelay-products` für die `products`-Tabelle

### Dead Letter Queue (DLQ) Unterstützung

ElasticRelay enthält ein umfassendes DLQ-System zur Behandlung fehlgeschlagener Events:

- **Automatischer Retry**: Fehlgeschlagene Events werden automatisch mit exponentiellem Backoff wiederholt
- **Persistenter Speicher**: DLQ-Elemente werden mit vollständiger Zustandsverwaltung auf die Festplatte gespeichert
- **Deduplizierung**: Verhindert, dass doppelte Events in die Warteschlange aufgenommen werden
- **Status-Tracking**: Vollständige Lebenszyklus-Verfolgung (ausstehend, wiederholt, erschöpft, gelöst, verworfen)
- **Manuelle Verwaltung**: Unterstützung für manuelle Element-Inspektion und -Verwaltung
- **Automatische Bereinigung**: Gelöste Elemente werden nach konfigurierbarer Dauer automatisch bereinigt

### PostgreSQL Unterstützung

ElasticRelay bietet umfassende PostgreSQL CDC-Funktionen mit erweiterten Features:

#### Kern PostgreSQL Features
- **Logische Replikation**: Nutzt PostgreSQL's native logische Replikation mit `pgoutput` Plugin
- **WAL-Parsing**: Erweitertes Write-Ahead Log Parsing für Echtzeit-Änderungserfassung
- **Replikationsslots**: Automatische Erstellung und Verwaltung von logischen Replikationsslots
- **Publications**: Dynamische Publication-Verwaltung für Tabellenfilterung
- **LSN-Verwaltung**: Präzise Log Sequence Number Verfolgung für Checkpoint/Resume-Funktionalität

#### Erweiterte PostgreSQL Funktionen
- **Connection Pooling**: Intelligente Verbindungspool-Verwaltung mit konfigurierbaren Limits
- **Parallele Snapshots**: Multi-Thread initiale Datensynchronisierung mit Chunking-Strategien
- **Typ-Mapping**: Umfassende PostgreSQL zu Elasticsearch Typ-Konvertierung einschließlich:
  - Alle numerischen Typen (bigint, integer, real, double, numeric)
  - Text- und Zeichentypen (text, varchar, char)
  - Datum/Zeit-Typen mit Zeitzonenunterstützung (timestamp, timestamptz, date, time)
  - JSON/JSONB mit nativem Objekt-Mapping
  - Array-Typen (integer arrays, text arrays)
  - Erweiterte Typen (UUID, bytea, inet, geometrische Typen)
- **Leistungsoptimierungen**: 
  - Adaptive Planung für große Tabellen
  - Streaming-Modus für Speichereffizienz
  - Konfigurierbare Batch-Größen und Worker-Pools
  - Verbindungslebenszyklus-Verwaltung

#### PostgreSQL Konfigurationsoptionen
```json
{
  "type": "postgresql",
  "options": {
    "ssl_mode": "disable|require|verify-ca|verify-full",
    "slot_name": "custom_replication_slot_name",
    "publication_name": "custom_publication_name",
    "batch_size": 1000,
    "max_connections": 10,
    "min_connections": 2,
    "parallel_snapshots": true,
    "enable_performance_monitoring": true
  }
}
```

### MongoDB Unterstützung

ElasticRelay bietet vollständige MongoDB CDC-Funktionen mit Change Streams:

#### Kern MongoDB Features
- **Change Streams**: Echtzeit-CDC mit MongoDB's nativer Change Streams API
- **Cluster-Unterstützung**: Automatische Erkennung und Unterstützung für Replica Sets und Sharded Clusters
- **Resume Tokens**: Persistentes Resume Token Management für Checkpoint/Resume-Funktionalität
- **Operations-Mapping**: Vollständige Unterstützung für INSERT, UPDATE, REPLACE und DELETE Operationen

#### Erweiterte MongoDB Funktionen
- **Sharded Cluster Unterstützung**: 
  - Multi-Shard Überwachung via mongos
  - Migrations-Bewusstsein für Konsistenz während Chunk-Migrationen
  - Chunk-Verteilungsüberwachung
- **Typ-Konvertierung**: Vollständige BSON zu JSON-freundliche Typ-Konvertierung:
  - ObjectID → string (Hex-Format)
  - DateTime → RFC3339 Zeitstempel
  - Decimal128 → string (Präzision erhalten)
  - Binary → base64 kodiert
  - Verschachtelte Dokumente mit konfigurierbarer Abflachungstiefe
- **Parallele Snapshots**: 
  - ObjectID-basiertes Chunking für Standard-Collections
  - Numerisches ID-basiertes Chunking für Integer-Primärschlüssel
  - Skip/Limit Fallback für komplexe ID-Typen

#### MongoDB Konfigurationsoptionen
```json
{
  "type": "mongodb",
  "host": "localhost",
  "port": 27017,
  "user": "elasticrelay_user",
  "password": "password",
  "database": "your_database",
  "options": {
    "auth_source": "admin",
    "replica_set": "rs0",
    "read_preference": "primaryPreferred",
    "batch_size": 1000,
    "flatten_depth": 3
  }
}
```

#### MongoDB Setup-Anforderungen
```sh
# MongoDB muss im Replica Set Modus für Change Streams laufen
# Verwenden Sie das bereitgestellte Setup-Skript:
./scripts/reset-mongodb.sh

# Oder mit Docker Compose:
docker-compose up -d mongodb
docker-compose up mongodb-init

# Überprüfen Sie, ob das Replica Set konfiguriert ist:
./scripts/verify-mongodb.sh
```

### Parallele Verarbeitung

Erweiterte parallele Snapshot-Verarbeitungsfähigkeiten:

- **Chunking-Strategien**: Unterstützung für ID-basiertes, zeitbasiertes und hash-basiertes Chunking
- **Worker-Pools**: Konfigurierbare Worker-Pool-Größen mit adaptiver Planung
- **Fortschrittsverfolgung**: Echtzeit-Fortschrittsüberwachung und Statistiken
- **Große Tabellen Unterstützung**: Optimierte Handhabung großer Tabellen mit intelligentem Chunking
- **Streaming-Modus**: Speichereffiziente Streaming-Verarbeitung für große Datensätze

## Aktueller Status

**Aktuelle Version**: v1.3.1 | **Phase**: Phase 2 Abgeschlossen ✅, Eintritt in Phase 3

Dieses Projekt hat seine Kern Multi-Source CDC Plattform (Phase 2) abgeschlossen und bereitet sich auf Enterprise-Grade Erweiterungen vor.

### ✅ Abgeschlossene Features (Phase 2 - v1.3.1)
- **Multi-Source CDC Pipeline**: 
  - **MySQL CDC**: Vollständige Implementierung mit binlog-basierter Echtzeit-Synchronisierung
  - **PostgreSQL CDC**: Vollständige logische Replikation mit WAL-Parsing, Replikationsslots und Publications
  - **MongoDB CDC**: Vollständige Change Streams Implementierung mit Replica Set und Sharded Cluster Unterstützung
- **Multi-Table Dynamische Indexierung**: Automatische Elasticsearch-Index-Erstellung und -Verwaltung pro Tabelle mit konfigurierbarer Benennung
- **gRPC Architektur**: Vollständige Service-Definitionen und Implementierungen (Connector, Orchestrator, Sink, Transform, Health)
- **Erweitertes Konfigurationsmanagement**: 
  - Multi-Source Konfigurationssystem mit Legacy-Migrationsunterstützung
  - Konfigurationssynchronisierung und Hot-Reload-Fähigkeiten
  - Automatische Formaterkennung und Migrationstools
- **Elasticsearch Integration**: Hochleistungs-Bulk-Schreiben mit automatischem Index-Management und Datenbereinigung
- **Checkpoint/Resume**: Persistente Positionsverfolgung für Fehlertoleranz mit automatischer Wiederherstellung (binlog, LSN, resume tokens)
- **Datentransformation**: Vollständige Pipeline für Datenverarbeitung und Governance (pass-through, vollständige Engine in Phase 3)
- **Dead Letter Queue (DLQ)**: 
  - Umfassendes DLQ-System mit exponentiellem Backoff-Retry (konfigurierbare max. Wiederholungen)
  - Persistenter Speicher mit Deduplizierung und Status-Tracking
  - Automatische Bereinigung gelöster Elemente
  - Unterstützung für manuelle Element-Verwaltung und -Inspektion
- **Parallele Verarbeitung**: 
  - Erweiterte parallele Snapshot-Verarbeitung mit Chunking-Strategien
  - Konfigurierbare Worker-Pools und adaptive Planung
  - Fortschrittsverfolgung und Statistiksammlung
  - Unterstützung für große Tabellenoptimierung (MySQL, PostgreSQL, MongoDB)
- **Versionsverwaltung**: Vollständiges Versionsinjektionssystem mit Build-Zeit-Metadaten
- **Robuste Fehlerbehandlung**: Umfassende Fehlerbehandlung mit Fallback-Mechanismen
- **Log-Level-Steuerung**: Zur Laufzeit konfigurierbare Protokollierung mit zentraler Verwaltung

### 🚧 In Arbeit (Phase 3 - v1.0-beta)
- **Transform Engine**: Vollständige Datentransformationsimplementierung (Feld-Mapping, Typ-Konvertierung, Ausdrücke, Maskierung)
- **Prometheus Metrics**: Vollständige Observability mit Metrik-Export
- **HTTP REST API**: grpc-gateway Integration mit OpenAPI-Dokumentation
- **Health Check Erweiterung**: Kubernetes-ready Readiness/Liveness Probes

### 📋 Geplant (Phase 4+)
- **Frontend-Entwicklung**: Control Plane GUI (TypeScript/Next.js)
- **Hochverfügbarkeit**: Multi-Replica Deployment mit automatischem Failover
- **Sicherheitserweiterung**: mTLS, RBAC und Audit-Logging
- **Erweiterte Governance**: Umfangreiche Datentransformationsregeln und feldbasierte Governance

---

## 📄 Lizenz

ElasticRelay ist unter der [Apache License 2.0](LICENSE) lizenziert.

```
Copyright 2024 上海悦高软件股份有限公司 (Shanghai Yogoo Software Co., Ltd.)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

## 🤝 Mitwirken

Wir freuen uns über Beiträge! Bitte sehen Sie unsere [Beitragsrichtlinien](CONTRIBUTING.md) für Details.

## 📞 Support

- 🐦 X (Twitter): [@ElasticRelay](https://x.com/ElasticRelay)
- 🌐 Offizielle Website: [www.elasticrelay.com](http://www.elasticrelay.com)
- 📧 E-Mail: support@yogoo.net
- 💬 Community: [GitHub Discussions](https://github.com/yogoosoft/ElasticRelay/discussions)
- 🐛 Fehlerberichte: [GitHub Issues](https://github.com/yogoosoft/ElasticRelay/issues)
- 📖 Dokumentation: [docs.elasticrelay.com](https://docs.elasticrelay.com)
