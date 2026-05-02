# Java Client for ScoriaDB

ScoriaDB provides a gRPC API, so you can use it from Java just like from any other language. This guide shows you how to connect, authenticate, perform CRUD operations, and work with transactions.


## Table of Contents

1. [When to Use the Java Client](#1-when-to-use-the-java-client)
2. [Prerequisites](#2-prerequisites)
3. [Maven Dependencies](#3-maven-dependencies)
   - 3.1. Gradle Dependencies
4. [Generate Java gRPC Code](#4-generate-java-grpc-code)
5. [Import and Connect](#5-import-and-connect)
6. [Authenticate and Get Token](#6-authenticate-and-get-token)
7. [Write a Key (Put)](#7-write-a-key-put)
8. [Read a Key (Get)](#8-read-a-key-get)
9. [Delete a Key (Delete)](#9-delete-a-key-delete)
10. [Scan Keys by Prefix (Scan)](#10-scan-keys-by-prefix-scan)
11. [Transactions](#11-transactions)
    - 11.1. Begin Transaction
    - 11.2. Prepare Operations
    - 11.3. Commit Transaction
12. [Full Java Client Example](#12-full-java-client-example)
13. [Error Handling](#13-error-handling)
14. [Method Reference](#14-method-reference)
15. [Summary](#15-summary)

---
## Prerequisites

Before you start, make sure you have:

- Java 11 or higher
- Maven or Gradle
- ScoriaDB server running (see [Server Mode](#5-using-as-a-server-cli) section)

---

## Maven Dependencies

Add the following to your `pom.xml`:

```xml
<dependencies>
    <!-- gRPC core libraries -->
    <dependency>
        <groupId>io.grpc</groupId>
        <artifactId>grpc-netty-shaded</artifactId>
        <version>1.59.0</version>
    </dependency>
    <dependency>
        <groupId>io.grpc</groupId>
        <artifactId>grpc-protobuf</artifactId>
        <version>1.59.0</version>
    </dependency>
    <dependency>
        <groupId>io.grpc</groupId>
        <artifactId>grpc-stub</artifactId>
        <version>1.59.0</version>
    </dependency>

    <!-- For javax.annotation (generated code) -->
    <dependency>
        <groupId>javax.annotation</groupId>
        <artifactId>javax.annotation-api</artifactId>
        <version>1.3.2</version>
    </dependency>
</dependencies>
```

### Gradle Dependencies

```gradle
dependencies {
    implementation 'io.grpc:grpc-netty-shaded:1.59.0'
    implementation 'io.grpc:grpc-protobuf:1.59.0'
    implementation 'io.grpc:grpc-stub:1.59.0'
    implementation 'javax.annotation:javax.annotation-api:1.3.2'
}
```

---

## Generate Java gRPC Code

**What is this?**  
Protocol Buffers (protobuf) is a language-neutral way to define APIs. ScoriaDB defines its API in a `.proto` file. You need to generate Java classes from this file so your Java code can talk to ScoriaDB.

**When to do this:**  
Once per ScoriaDB version update, or when you first set up the project.

Using Maven (add to `pom.xml`):

```xml
<build>
    <extensions>
        <extension>
            <groupId>kr.motd.maven</groupId>
            <artifactId>os-maven-plugin</artifactId>
            <version>1.7.1</version>
        </extension>
    </extensions>
    <plugins>
        <plugin>
            <groupId>org.xolstice.maven.plugins</groupId>
            <artifactId>protobuf-maven-plugin</artifactId>
            <version>0.6.1</version>
            <configuration>
                <protocArtifact>com.google.protobuf:protoc:3.24.0:exe:${os.detected.classifier}</protocArtifact>
                <pluginId>grpc-java</pluginId>
                <pluginArtifact>io.grpc:protoc-gen-grpc-java:1.59.0:exe:${os.detected.classifier}</pluginArtifact>
            </configuration>
            <executions>
                <execution>
                    <goals>
                        <goal>compile</goal>
                        <goal>compile-custom</goal>
                    </goals>
                </execution>
            </executions>
        </plugin>
    </plugins>
</build>
```

Or manually with protoc:

```bash
# Clone the repository
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB

# Generate Java classes
protoc --java_out=./src/main/java \
       --grpc-java_out=./src/main/java \
       ./proto/scoriadb.proto
```

---

## Import and Connect

**What this does:**  
Establishes a network connection to the ScoriaDB server. All subsequent operations use this connection.

**When to use:**  
Once at application startup. Reuse the same channel for all operations – gRPC channels are thread-safe and designed for reuse.

```java
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import scoriadb.ScoriaDBGrpc;

// Create a connection to the server
ManagedChannel channel = ManagedChannelBuilder.forAddress("localhost", 50051)
        .usePlaintext()
        .build();

// Create client stub
ScoriaDBGrpc.ScoriaDBBlockingStub stub = ScoriaDBGrpc.newBlockingStub(channel);
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `host` | `localhost` | Server address |
| `port` | `50051` | gRPC port |
| `usePlaintext()` | – | Disable TLS (use for development only) |
| `channel` | – | gRPC connection object. Reuse across requests |
| `stub` | – | Client stub for making blocking calls |

> **Note:** Use `.useTransportSecurity()` with SSL credentials in production. The examples use `usePlaintext()` for simplicity.

---

## Authenticate and Get Token

**What this does:**  
Sends your username and password to the server. On success, the server returns a JWT (JSON Web Token). You must include this token in all subsequent requests.

**When to use:**  
Immediately after connecting, before doing any other operations. The token expires after 24 hours (configurable).

```java
import io.grpc.Metadata;
import io.grpc.stub.MetadataUtils;
import scoriadb.AuthRequest;
import scoriadb.AuthResponse;

// Authenticate
AuthRequest authRequest = AuthRequest.newBuilder()
        .setUsername("admin")
        .setPassword("admin")
        .build();

AuthResponse authResponse = stub.authenticate(authRequest);
String token = authResponse.getJwtToken();

// Attach token to all subsequent calls
Metadata metadata = new Metadata();
Metadata.Key<String> authKey = Metadata.Key.of("authorization", 
        Metadata.ASCII_STRING_MARSHALLER);
metadata.put(authKey, "Bearer " + token);

// Create a new stub with the token attached
stub = stub.withInterceptors(MetadataUtils.newAttachHeadersInterceptor(metadata));
```

| Field | Type | Description |
|-------|------|-------------|
| `username` | string | User name (default: `admin` on first start) |
| `password` | string | User password (default: `admin` on first start) |
| `jwtToken` | string | Token to use in `authorization` header for all future calls |

**What is JWT?**  
JWT is a signed token that proves your identity. The server verifies the signature and extracts your roles (admin, readwrite, readonly) from it.

---

## Write a Key (Put)

**What this does:**  
Stores a key-value pair in the database. If the key already exists, it overwrites the value (MVCC creates a new version).

**When to use:**  
Every time you need to save or update data.

```java
import com.google.protobuf.ByteString;
import scoriadb.PutRequest;

PutRequest putRequest = PutRequest.newBuilder()
        .setKey(ByteString.copyFromUtf8("username"))
        .setValue(ByteString.copyFromUtf8("alice"))
        .setCfName("default")
        .build();

stub.put(putRequest);
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `key` | `ByteString` | Yes | Use `ByteString.copyFromUtf8()` for strings |
| `value` | `ByteString` | Yes | Data to store. Can be text, JSON, binary |
| `cfName` | `String` | No | Column Family (default: `"default"`). Use different CFs to separate data types |

---

## Read a Key (Get)

**What this does:**  
Retrieves the value for a given key. Returns the latest version (highest commit timestamp).

**When to use:**  
Whenever you need to fetch data by its exact key.

```java
import scoriadb.GetRequest;
import scoriadb.GetResponse;

GetRequest getRequest = GetRequest.newBuilder()
        .setKey(ByteString.copyFromUtf8("username"))
        .setCfName("default")
        .build();

GetResponse response = stub.get(getRequest);

if (response.getFound()) {
    String value = response.getValue().toStringUtf8();
    System.out.println("Value: " + value);
} else {
    System.out.println("Key not found");
}
```

| Field | Type | Description |
|-------|------|-------------|
| `found` | `boolean` | `true` if the key exists, `false` otherwise |
| `value` | `ByteString` | Stored value (if found). Use `.toStringUtf8()` for text |

---

## Delete a Key (Delete)

**What this does:**  
Removes a key from the database. Actually creates a "tombstone" version – the key is no longer visible but the old version may still exist until compaction runs.

**When to use:**  
When you no longer need a piece of data.

```java
import scoriadb.DeleteRequest;

DeleteRequest deleteRequest = DeleteRequest.newBuilder()
        .setKey(ByteString.copyFromUtf8("username"))
        .setCfName("default")
        .build();

stub.delete(deleteRequest);
```

---

## Scan Keys by Prefix (Scan)

**What this does:**  
Finds all keys that start with a given prefix. Returns them as a stream (one message per key-value pair). Useful for iteration without loading everything into memory at once.

**When to use:**
- You need to find multiple keys (e.g., all users with prefix `user:`)
- You don't know the exact key names
- You are building a search or list feature

```java
import scoriadb.ScanRequest;
import scoriadb.ScanResponse;

ScanRequest scanRequest = ScanRequest.newBuilder()
        .setPrefix(ByteString.copyFromUtf8("user:"))
        .setCfName("default")
        .build();

System.out.println("Scan results:");
Iterator<ScanResponse> iterator = stub.scan(scanRequest);

while (iterator.hasNext()) {
    ScanResponse response = iterator.next();
    String key = response.getKey().toStringUtf8();
    String value = response.getValue().toStringUtf8();
    System.out.println("  " + key + " → " + value);
}
```

| Field | Type | Description |
|-------|------|-------------|
| `prefix` | `ByteString` | Only keys starting with this prefix. Empty returns all keys |
| `cfName` | `String` | Column Family (default: `"default"`) |

**How streaming works:**  
The server sends results one by one. The `Iterator` receives each key-value pair as it becomes available. This is memory-efficient for large result sets.

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

```java
import scoriadb.BeginTxnRequest;
import scoriadb.BeginTxnResponse;

BeginTxnRequest beginRequest = BeginTxnRequest.newBuilder().build();
BeginTxnResponse beginResponse = stub.beginTxn(beginRequest);
String txnId = beginResponse.getTxnId();
```

| Field | Type | Description |
|-------|------|-------------|
| `txnId` | `String` | Unique transaction identifier. Use this in `CommitTxn` |
| `startTs` | `long` | Snapshot timestamp for this transaction (for debugging) |

### Step 2: Prepare Operations

**What this does:**  
Builds the list of operations that will be executed atomically.

```java
import scoriadb.TxnOp;
import scoriadb.TxnOp.OpType;

List<TxnOp> operations = new ArrayList<>();

// Add a Put operation
TxnOp putOp = TxnOp.newBuilder()
        .setOp(OpType.PUT)
        .setKey(ByteString.copyFromUtf8("balance"))
        .setValue(ByteString.copyFromUtf8("100"))
        .build();
operations.add(putOp);

// Add a Delete operation
TxnOp deleteOp = TxnOp.newBuilder()
        .setOp(OpType.DELETE)
        .setKey(ByteString.copyFromUtf8("temp"))
        .build();
operations.add(deleteOp);
```

| `OpType` | Description |
|----------|-------------|
| `PUT` | Store a key-value pair |
| `DELETE` | Remove a key |

### Step 3: Commit

**What this does:**  
Applies all operations atomically. If any key was modified by another transaction after `beginTxn`, the commit fails with `ABORTED` status.

**When to retry:**  
If you get `ABORTED`, wait a short time (e.g., 10ms) and retry the entire transaction from `beginTxn`.

```java
import scoriadb.CommitTxnRequest;

CommitTxnRequest commitRequest = CommitTxnRequest.newBuilder()
        .setTxnId(txnId)
        .addAllOps(operations)
        .build();

stub.commitTxn(commitRequest);
```

---

## Error Handling

**What are gRPC errors?**  
When something goes wrong, gRPC throws a `StatusRuntimeException` with a status code. Your code should catch these and react appropriately.

**When to use error handling:**  
Always wrap gRPC calls in try-catch blocks, especially in production.

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

Example with retry logic:

```java
public void commitTransactionWithRetry(String txnId, List<TransactionOp> operations, int maxRetries) {
    for (int attempt = 0; attempt < maxRetries; attempt++) {
        try {
            commitTransaction(txnId, operations);
            return;
        } catch (StatusRuntimeException e) {
            if (e.getStatus().getCode() == Status.Code.ABORTED) {
                System.out.println("Conflict, retrying (" + (attempt + 1) + "/" + maxRetries + ")");
                try {
                    Thread.sleep(10 * (1 << attempt)); // exponential backoff
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                    throw new RuntimeException(ie);
                }
                continue;
            }
            throw e;
        }
    }
    throw new RuntimeException("Transaction failed after " + maxRetries + " retries");
}
```

---

## Method Reference

| Method | Java call | When to use |
|--------|-----------|-------------|
| `authenticate` | `stub.authenticate(AuthRequest)` | Once at startup, before any other call |
| `put` | `stub.put(PutRequest)` | Every time you need to save or update data |
| `get` | `stub.get(GetRequest)` | When you know the exact key |
| `delete` | `stub.delete(DeleteRequest)` | When a key is no longer needed |
| `scan` | `stub.scan(ScanRequest)` | When you need to find keys by prefix or list multiple keys |
| `beginTxn` | `stub.beginTxn(BeginTxnRequest)` | Before a series of operations that must be atomic |
| `commitTxn` | `stub.commitTxn(CommitTxnRequest)` | After preparing operations for a transaction |

---

## Summary

| Step | What | Why |
|------|------|-----|
| 1 | Add Maven/Gradle dependencies | Include gRPC libraries |
| 2 | Generate Java code from `.proto` | Create client classes |
| 3 | Create `ManagedChannel` and `BlockingStub` | Establish network connection |
| 4 | Call `authenticate` | Get JWT token |
| 5 | Attach token to interceptor | Authorize all subsequent calls |
| 6 | Use `put`/`get`/`delete`/`scan` | Work with data |
| 7 | Use `beginTxn` + `commitTxn` | Group operations atomically |
| 8 | Handle `StatusRuntimeException` | Deal with failures gracefully |

The Java client is best suited for:

- Spring Boot / Micronaut / Quarkus applications
- Android applications (with appropriate networking permissions)
- Enterprise Java backends
- Integration with existing Java codebases

For maximum performance, use the Go embedded API instead. For other languages, the same gRPC pattern applies.