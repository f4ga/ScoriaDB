# Python Client for ScoriaDB

ScoriaDB provides a gRPC API, so you can use it from Python just like from any other language. This guide shows you how to connect, authenticate, perform CRUD operations, and work with transactions.

**Quick links:** [GitHub](https://github.com/f4ga/ScoriaDB) | [Main Documentation](../README.md)

---

## Table of Contents

1. [When to Use the Python Client](#1-when-to-use-the-python-client)
2. [Prerequisites](#2-prerequisites)
3. [Generate Python gRPC Code](#3-generate-python-grpc-code)
4. [Import and Connect](#4-import-and-connect)
5. [Authenticate and Get Token](#5-authenticate-and-get-token)
6. [Write a Key (Put)](#6-write-a-key-put)
7. [Read a Key (Get)](#7-read-a-key-get)
8. [Delete a Key (Delete)](#8-delete-a-key-delete)
9. [Scan Keys by Prefix (Scan)](#9-scan-keys-by-prefix-scan)
10. [Transactions](#10-transactions)
    - 10.1. Begin Transaction
    - 10.2. Prepare Operations
    - 10.3. Commit Transaction
11. [Full Python Client Example](#11-full-python-client-example)
12. [Error Handling](#12-error-handling)
13. [Method Reference](#13-method-reference)
14. [Summary](#14-summary)

---
## Prerequisites

Before you start, make sure you have:

- Python 3.8 or higher
- ScoriaDB server running (see [Server Mode](#5-using-as-a-server-cli) section)

Install the required packages:

```bash
pip install grpcio grpcio-tools
```

---

## Generate Python gRPC Code

**What is this?**  
Protocol Buffers (protobuf) is a language-neutral way to define APIs. ScoriaDB defines its API in a `.proto` file. You need to generate Python classes from this file so your Python code can talk to ScoriaDB.

**When to do this:**  
Once per ScoriaDB version update, or when you first set up the project.

```bash
# Clone the repository (or copy proto/scoriadb.proto from it)
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB

# Generate Python classes
python -m grpc_tools.protoc \
    -I./proto \
    --python_out=. \
    --grpc_python_out=. \
    ./proto/scoriadb.proto
```

This creates two files:
- `scoriadb_pb2.py` – data structures (messages like PutRequest, GetResponse)
- `scoriadb_pb2_grpc.py` – gRPC client and server classes

---

## Import and Connect

**What this does:**  
Establishes a network connection to the ScoriaDB server. All subsequent operations use this connection.

**When to use:**  
Once at application startup. Reuse the same connection for all operations – gRPC channels are thread-safe and designed for reuse.

```python
import grpc
import scoriadb_pb2
import scoriadb_pb2_grpc

# Create a connection to the server
channel = grpc.insecure_channel('localhost:50051')
stub = scoriadb_pb2_grpc.ScoriaDBStub(channel)
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `host` | `localhost:50051` | Server address (gRPC port) |
| `channel` | – | gRPC connection object. Reuse this across requests |
| `stub` | – | Client stub for making calls. Also reusable |

> **Note:** Use `grpc.secure_channel()` if you enable TLS in production. The examples use `insecure_channel` for simplicity.

---

## Authenticate and Get Token

**What this does:**  
Sends your username and password to the server. On success, the server returns a JWT (JSON Web Token). You must include this token in all subsequent requests.

**When to use:**  
Immediately after connecting, before doing any other operations. The token expires after 24 hours (configurable).

```python
# Authenticate
auth_request = scoriadb_pb2.AuthRequest(
    username="admin",
    password="admin"
)
auth_response = stub.Authenticate(auth_request)
token = auth_response.jwt_token

# Attach token to all subsequent calls
metadata = (('authorization', f'Bearer {token}'),)
```

| Field | Type | Description |
|-------|------|-------------|
| `username` | string | User name (default: `admin` on first start) |
| `password` | string | User password (default: `admin` on first start) |
| `jwt_token` | string | Token to use in `authorization` header for all future calls |

**What is JWT?**  
JWT is a signed token that proves your identity. The server verifies the signature and extracts your roles (admin, readwrite, readonly) from it.

---

## Write a Key (Put)

**What this does:**  
Stores a key-value pair in the database. If the key already exists, it overwrites the value (MVCC creates a new version).

**When to use:**  
Every time you need to save or update data.

```python
put_request = scoriadb_pb2.PutRequest(
    key=b"username",
    value=b"alice",
    cf_name="default"
)
stub.Put(put_request, metadata=metadata)
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `key` | bytes | Yes | Unique identifier. Use `b"string"` or `.encode('utf-8')` |
| `value` | bytes | Yes | Data to store. Can be text, JSON, binary, etc. |
| `cf_name` | string | No | Column Family (default: `"default"`). Use different CFs to separate data types. |

---

## Read a Key (Get)

**What this does:**  
Retrieves the value for a given key. Returns the latest version (highest commit timestamp).

**When to use:**  
Whenever you need to fetch data by its exact key.

```python
get_request = scoriadb_pb2.GetRequest(
    key=b"username",
    cf_name="default"
)
response = stub.Get(get_request, metadata=metadata)

if response.found:
    print("Value:", response.value.decode('utf-8'))
else:
    print("Key not found")
```

| Field | Type | Description |
|-------|------|-------------|
| `found` | bool | `True` if the key exists, `False` otherwise |
| `value` | bytes | Stored value (if found). Decode with `.decode('utf-8')` for text |

---

## Delete a Key (Delete)

**What this does:**  
Removes a key from the database. Actually creates a "tombstone" version – the key is no longer visible but the old version may still exist until compaction runs.

**When to use:**  
When you no longer need a piece of data.

```python
delete_request = scoriadb_pb2.DeleteRequest(
    key=b"username",
    cf_name="default"
)
stub.Delete(delete_request, metadata=metadata)
```

---

## Scan Keys by Prefix (Scan)

**What this does:**  
Finds all keys that start with a given prefix. Returns them as a stream (one message per key-value pair). Useful for iteration without loading everything into memory at once.

**When to use:**
- You need to find multiple keys (e.g., all users with prefix `user:`)
- You don't know the exact key names
- You are building a search or list feature

```python
scan_request = scoriadb_pb2.ScanRequest(
    prefix=b"user:",
    cf_name="default"
)

print("Scan results:")
for response in stub.Scan(scan_request, metadata=metadata):
    key = response.key.decode('utf-8')
    value = response.value.decode('utf-8')
    print(f"  {key} → {value}")
```

| Field | Type | Description |
|-------|------|-------------|
| `prefix` | bytes | Only keys starting with this prefix. Empty `b""` returns all keys |
| `cf_name` | string | Column Family (default: `"default"`) |

**How streaming works:**  
The server sends results one by one. The `for response in stub.Scan(...)` loop receives each key-value pair as it becomes available. This is memory-efficient for large result sets.

---

## Transactions

**What this does:**  
Groups multiple operations (puts and deletes) into a single atomic unit. Either all operations succeed, or none are applied.

**When to use:**
- You need to update multiple keys at once
- Consistency matters (e.g., transferring money between accounts)
- You want to read several keys and then update them based on their current values

### Step 1: Begin Transaction

**What this does:**  
Starts a transaction and returns a unique transaction ID. The transaction sees a snapshot of the database at this moment.

```python
begin_request = scoriadb_pb2.BeginTxnRequest()
begin_response = stub.BeginTxn(begin_request, metadata=metadata)
txn_id = begin_response.txn_id
```

| Field | Type | Description |
|-------|------|-------------|
| `txn_id` | string | Unique transaction identifier. Use this in `CommitTxn` |
| `start_ts` | int64 | Snapshot timestamp for this transaction (for debugging) |

### Step 2: Prepare Operations

**What this does:**  
Builds the list of operations that will be executed atomically.

```python
operations = []

# Add a Put operation
op1 = scoriadb_pb2.TxnOp(
    op=scoriadb_pb2.TxnOp.PUT,
    key=b"balance",
    value=b"100",
    cf_name="default"
)
operations.append(op1)

# Add a Delete operation
op2 = scoriadb_pb2.TxnOp(
    op=scoriadb_pb2.TxnOp.DELETE,
    key=b"temp",
    cf_name="default"
)
operations.append(op2)
```

| `TxnOp.OpType` | Value | Description |
|----------------|-------|-------------|
| `PUT` | 0 | Store a key-value pair |
| `DELETE` | 1 | Remove a key |

### Step 3: Commit

**What this does:**  
Applies all operations atomically. If any key was modified by another transaction after `BeginTxn`, the commit fails with `ABORTED` status.

**When to retry:**  
If you get `ABORTED`, wait a short time (e.g., 10ms) and retry the entire transaction from `BeginTxn`.

```python
commit_request = scoriadb_pb2.CommitTxnRequest(
    txn_id=txn_id,
    ops=operations
)
stub.CommitTxn(commit_request, metadata=metadata)
```

---

## Full Python Client Example

```python
#!/usr/bin/env python3
"""
ScoriaDB Python Client
Complete example with error handling
"""

import grpc
import scoriadb_pb2
import scoriadb_pb2_grpc
import time


class ScoriaDBClient:
    """Python client for ScoriaDB key-value database"""

    def __init__(self, address="localhost:50051"):
        """Create a new client instance"""
        self.address = address
        self.channel = None
        self.stub = None
        self.token = None
        self._connect()

    def _connect(self):
        """Establish gRPC connection (call once at startup)"""
        self.channel = grpc.insecure_channel(self.address)
        self.stub = scoriadb_pb2_grpc.ScoriaDBStub(self.channel)

    def login(self, username, password):
        """Authenticate and obtain JWT token (call before any other operations)"""
        request = scoriadb_pb2.AuthRequest(
            username=username,
            password=password
        )
        try:
            response = self.stub.Authenticate(request)
            self.token = response.jwt_token
            return self.token
        except grpc.RpcError as e:
            print(f"Authentication failed: {e.code()} - {e.details()}")
            raise

    def _metadata(self):
        """Return authorization metadata for gRPC calls (internal use)"""
        if not self.token:
            raise Exception("Not authenticated. Call login() first.")
        return (('authorization', f'Bearer {self.token}'),)

    def put(self, key, value, cf="default"):
        """Store a key-value pair"""
        request = scoriadb_pb2.PutRequest(
            key=key.encode('utf-8'),
            value=value.encode('utf-8'),
            cf_name=cf
        )
        self.stub.Put(request, metadata=self._metadata())

    def get(self, key, cf="default"):
        """Retrieve value for a key. Returns None if not found."""
        request = scoriadb_pb2.GetRequest(
            key=key.encode('utf-8'),
            cf_name=cf
        )
        response = self.stub.Get(request, metadata=self._metadata())
        return response.value.decode('utf-8') if response.found else None

    def delete(self, key, cf="default"):
        """Delete a key"""
        request = scoriadb_pb2.DeleteRequest(
            key=key.encode('utf-8'),
            cf_name=cf
        )
        self.stub.Delete(request, metadata=self._metadata())

    def scan(self, prefix="", cf="default"):
        """Scan keys with given prefix. Returns list of (key, value) tuples."""
        request = scoriadb_pb2.ScanRequest(
            prefix=prefix.encode('utf-8'),
            cf_name=cf
        )
        results = []
        for response in self.stub.Scan(request, metadata=self._metadata()):
            key = response.key.decode('utf-8')
            value = response.value.decode('utf-8')
            results.append((key, value))
        return results

    def begin_transaction(self):
        """Start a new transaction. Returns transaction ID."""
        request = scoriadb_pb2.BeginTxnRequest()
        response = self.stub.BeginTxn(request, metadata=self._metadata())
        return response.txn_id

    def commit_transaction(self, txn_id, operations):
        """Commit a transaction. Operations: list of ('put', key, value) or ('delete', key)"""
        ops = []
        for op in operations:
            if op[0] == 'put':
                _, key, value = op
                txn_op = scoriadb_pb2.TxnOp(
                    op=scoriadb_pb2.TxnOp.PUT,
                    key=key.encode('utf-8'),
                    value=value.encode('utf-8')
                )
            elif op[0] == 'delete':
                _, key = op
                txn_op = scoriadb_pb2.TxnOp(
                    op=scoriadb_pb2.TxnOp.DELETE,
                    key=key.encode('utf-8')
                )
            else:
                continue
            ops.append(txn_op)

        request = scoriadb_pb2.CommitTxnRequest(
            txn_id=txn_id,
            ops=ops
        )
        self.stub.CommitTxn(request, metadata=self._metadata())

    def close(self):
        """Close the gRPC channel (call once at shutdown)"""
        if self.channel:
            self.channel.close()


# ============================================
# Usage Example
# ============================================

def main():
    # Create client (connects immediately)
    client = ScoriaDBClient("localhost:50051")

    # Authenticate (admin/admin on first start)
    try:
        client.login("admin", "admin")
        print("✅ Connected and authenticated")
    except grpc.RpcError as e:
        print(f"Cannot connect to server: {e}")
        return

    # Write data
    client.put("user:1", "Alice")
    client.put("user:2", "Bob")
    print("✅ Wrote user:1 and user:2")

    # Read data
    user1 = client.get("user:1")
    print(f"📖 user:1 = {user1}")

    # Delete a key
    client.delete("user:2")
    print("✅ Deleted user:2")

    # Scan all keys
    all_keys = client.scan()
    print(f"📖 Total keys: {len(all_keys)}")
    for key, value in all_keys:
        print(f"  {key} → {value}")

    # Transaction example
    txn_id = client.begin_transaction()
    print(f"📖 Transaction started: {txn_id}")

    client.commit_transaction(txn_id, [
        ('put', 'txn_key', 'txn_value'),
        ('delete', 'temp_key')
    ])
    print("✅ Transaction committed")

    # Clean up
    client.close()


if __name__ == "__main__":
    main()
```

---

## Error Handling

**What are gRPC errors?**  
When something goes wrong, gRPC raises an `RpcError` with a status code. Your code should catch these and react appropriately.

**When to use error handling:**  
Always wrap gRPC calls in try/except blocks, especially in production.

Common status codes:

| Code | Description | How to handle |
|------|-------------|---------------|
| `UNAUTHENTICATED` | Invalid or missing token | Check credentials or token; re-authenticate |
| `PERMISSION_DENIED` | Role does not allow this operation | Use a user with higher privileges (admin for admin operations) |
| `NOT_FOUND` | Key, CF, or user does not exist | Verify the name before using it |
| `ABORTED` | Transaction conflict | Retry the entire transaction (wait 10-100ms first) |
| `UNAVAILABLE` | Server not reachable | Check if server is running; retry later |
| `DEADLINE_EXCEEDED` | Operation took too long | Increase timeout or reduce data size |
| `INTERNAL` | Server-side error | Check server logs; report bug |

Example:

```python
import grpc
import time

def retry_transaction(client, txn_id, operations, max_retries=3):
    """Commit transaction with automatic retry on conflict"""
    for attempt in range(max_retries):
        try:
            client.commit_transaction(txn_id, operations)
            return True
        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.ABORTED:
                print(f"Conflict, retrying ({attempt+1}/{max_retries})")
                time.sleep(0.01 * (2 ** attempt))  # exponential backoff
                continue
            raise
    return False
```

---

## Method Reference

| Method | Python call | When to use |
|--------|-------------|-------------|
| `Authenticate` | `stub.Authenticate(AuthRequest)` | Once at startup, before any other call |
| `Put` | `stub.Put(PutRequest, metadata=...)` | Every time you need to save or update data |
| `Get` | `stub.Get(GetRequest, metadata=...)` | When you know the exact key |
| `Delete` | `stub.Delete(DeleteRequest, metadata=...)` | When a key is no longer needed |
| `Scan` | `stub.Scan(ScanRequest, metadata=...)` | When you need to find keys by prefix or list multiple keys |
| `BeginTxn` | `stub.BeginTxn(BeginTxnRequest, metadata=...)` | Before a series of operations that must be atomic |
| `CommitTxn` | `stub.CommitTxn(CommitTxnRequest, metadata=...)` | After preparing operations for a transaction |

---

## Summary

| Step | What | Why |
|------|------|-----|
| 1 | `pip install grpcio grpcio-tools` | Install required packages |
| 2 | Generate Python code from `.proto` | Create client classes |
| 3 | Connect to server using `insecure_channel` | Establish network connection |
| 4 | Call `Authenticate` | Get JWT token |
| 5 | Add token to `metadata` | Authorize all subsequent calls |
| 6 | Use `Put`/`Get`/`Delete`/`Scan` | Work with data |
| 7 | Use `BeginTxn` + `CommitTxn` | Group operations atomically |
| 8 | Handle `grpc.RpcError` | Deal with failures gracefully |

The Python client is best suited for:

- Web applications (Django, Flask, FastAPI)
- Data processing pipelines (ETL, analytics)
- Scripts and automation tools
- Prototyping and testing

For maximum performance, use the Go embedded API instead. For other languages, the same gRPC pattern applies.