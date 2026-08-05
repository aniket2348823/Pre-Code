# ADR-001: Multi-Layer Memory System

## Status
Accepted

## Date
2024-01-15

## Context
VigilAgent needs a memory system that allows AI agents to recall past interactions, learn from codebase patterns, and maintain working context during sessions. The system must scale horizontally and support vector similarity search.

## Decision
We will implement a three-layer memory architecture:

### 1. Working Memory (Per-Session)
- **Implementation**: In-memory with optional Redis backing for persistence
- **TTL**: 30 minutes (default), configurable per session
- **Use case**: Current conversation context, tool call results
- **Access pattern**: O(1) reads, append-only writes

### 2. Episodic Memory (Past Interactions)
- **Implementation**: PostgreSQL + pgvector for vector similarity search
- **TTL**: 7 days (configurable), with importance-based eviction
- **Use case**: Past task executions, user decisions, error patterns
- **Features**:
  - Semantic search via embeddings
  - Importance scoring with time-based decay
  - Deduplication to prevent storage bloat
  - Compression for large content

### 3. Semantic Memory (Codebase Patterns)
- **Implementation**: PostgreSQL + pgvector with HNSW index support
- **TTL**: 30 days (configurable), with confidence-based eviction
- **Use case**: Code patterns, architecture decisions, learned workflows
- **Features**:
  - Vector similarity search
  - Observation count tracking
  - Confidence scoring with decay
  - File pattern association

## Consequences

### Positive
- Cascading recall (working → episodic → semantic) provides comprehensive context
- Vector similarity search enables semantic understanding
- TTL and eviction prevent unbounded growth
- Redis backing ensures session persistence across restarts

### Negative
- pgvector adds operational complexity (extension must be installed)
- Embedding generation adds latency to memory storage
- Requires careful tuning of similarity thresholds

## Alternatives Considered
1. **Redis-only**: Rejected - limited vector search capabilities
2. **Single PostgreSQL table**: Rejected - different access patterns need different schemas
3. **External vector DB (Pinecone/Weaviate)**: Rejected - adds cost and operational overhead

## References
- PostgreSQL pgvector: https://github.com/pgvector/pgvector
- HNSW algorithm: https://arxiv.org/abs/1603.09320
