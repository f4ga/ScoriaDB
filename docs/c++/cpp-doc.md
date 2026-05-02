# C++ Client for ScoriaDB

ScoriaDB provides a gRPC API, so you can use it from C++ just like from any other language. This guide shows you how to connect, authenticate, perform CRUD operations, and work with transactions.
# C++ Client for ScoriaDB

**Quick links:** [GitHub](https://github.com/f4ga/ScoriaDB) | [Main Documentation](../README.md)

---

## Table of Contents

1. [When to Use the C++ Client](#1-when-to-use-the-c-client)
2. [Prerequisites](#2-prerequisites)
   - 2.1. Install gRPC and Protocol Buffers
3. [Generate C++ gRPC Code](#3-generate-c-grpc-code)
4. [CMake Configuration](#4-cmake-configuration)
5. [Import and Connect](#5-import-and-connect)
6. [Helper Function for Metadata](#6-helper-function-for-metadata)
7. [Authenticate and Get Token](#7-authenticate-and-get-token)
8. [Write a Key (Put)](#8-write-a-key-put)
9. [Read a Key (Get)](#9-read-a-key-get)
10. [Delete a Key (Delete)](#10-delete-a-key-delete)
11. [Scan Keys by Prefix (Scan)](#11-scan-keys-by-prefix-scan)
12. [Transactions](#12-transactions)
    - 12.1. Begin Transaction
    - 12.2. Commit Transaction
13. [Full C++ Client Example](#13-full-c-client-example)
14. [Compiling the Example](#14-compiling-the-example)
15. [Error Handling](#15-error-handling)
16. [Method Reference](#16-method-reference)
17. [Summary](#17-summary)

---
## Prerequisites

Before you start, make sure you have:

- C++17 or higher compiler (gcc, clang, MSVC)
- CMake 3.15 or higher
- gRPC and Protocol Buffers installed
- ScoriaDB server running (see [Server Mode](#5-using-as-a-server-cli) section)

### Install gRPC and Protocol Buffers

**Ubuntu/Debian:**

```bash
sudo apt install -y build-essential autoconf libtool pkg-config cmake
sudo apt install -y libgrpc++-dev libprotobuf-dev protobuf-compiler-grpc
```

**macOS (Homebrew):**

```bash
brew install grpc protobuf cmake
```

**Fedora/RHEL:**

```bash
sudo dnf install grpc-devel protobuf-devel protobuf-compiler-grpc cmake
```

**From source (if packages are not available):**

```bash
git clone --recurse-submodules -b v1.59.0 https://github.com/grpc/grpc
cd grpc
mkdir -p cmake/build
cd cmake/build
cmake -DgRPC_INSTALL=ON -DgRPC_BUILD_TESTS=OFF ../..
make -j$(nproc)
sudo make install
```

---

## Generate C++ gRPC Code

**What is this?**  
Protocol Buffers (protobuf) is a language-neutral way to define APIs. ScoriaDB defines its API in a `.proto` file. You need to generate C++ classes from this file so your C++ code can talk to ScoriaDB.

**When to do this:**  
Once per ScoriaDB version update, or when you first set up the project.

```bash
# Clone the repository
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB

# Generate C++ gRPC code
mkdir -p cpp_client/generated
protoc -I./proto --cpp_out=./cpp_client/generated --grpc_out=./cpp_client/generated \
    --plugin=protoc-gen-grpc=`which grpc_cpp_plugin` \
    ./proto/scoriadb.proto
```

This creates four files in `cpp_client/generated/`:
- `scoriadb.pb.h` – data structure headers
- `scoriadb.pb.cc` – data structure implementations
- `scoriadb.grpc.pb.h` – gRPC client/server headers
- `scoriadb.grpc.pb.cc` – gRPC client/server implementations

---

## CMake Configuration

Create a `CMakeLists.txt` file for your C++ client:

```cmake
cmake_minimum_required(VERSION 3.15)
project(ScoriaDBClient)

set(CMAKE_CXX_STANDARD 17)

# Find gRPC and Protobuf packages
find_package(Protobuf REQUIRED)
find_package(gRPC CONFIG REQUIRED)

# Path to generated files
set(GENERATED_DIR ${CMAKE_CURRENT_SOURCE_DIR}/generated)

# Generated protobuf sources
set(PROTO_SRCS
    ${GENERATED_DIR}/scoriadb.pb.cc
    ${GENERATED_DIR}/scoriadb.grpc.pb.cc
)

# Create executable
add_executable(scoria_client main.cpp ${PROTO_SRCS})

# Link libraries
target_link_libraries(scoria_client
    ${Protobuf_LIBRARIES}
    gRPC::grpc++
    gRPC::grpc++_reflection
)

target_include_directories(scoria_client PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}
    ${GENERATED_DIR}
    ${Protobuf_INCLUDE_DIRS}
)
```

---

## Import and Connect

**What this does:**  
Establishes a network connection to the ScoriaDB server. All subsequent operations use this connection.

**When to use:**  
Once at application startup. Reuse the same channel for all operations – gRPC channels are thread-safe and designed for reuse.

```cpp
#include <memory>
#include <grpcpp/grpcpp.h>
#include "generated/scoriadb.grpc.pb.h"

// Create a channel to the server
auto channel = grpc::CreateChannel(
    "localhost:50051",
    grpc::InsecureChannelCredentials()
);

// Create client stub
auto stub = scoriadb::ScoriaDB::NewStub(channel);
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `host:port` | `localhost:50051` | Server address and gRPC port |
| `channel_credentials` | `InsecureChannelCredentials()` | Use for development only |
| `stub` | – | Client stub for making calls. Reusable |

> **Note:** Use `grpc::SslCredentials()` with SSL certificates in production. The examples use `InsecureChannelCredentials()` for simplicity.

---

## Authenticate and Get Token

**What this does:**  
Sends your username and password to the server. On success, the server returns a JWT (JSON Web Token). You must include this token in all subsequent requests.

**When to use:**  
Immediately after connecting, before doing any other operations. The token expires after 24 hours (configurable).

```cpp
#include <grpcpp/grpcpp.h>
#include "generated/scoriadb.pb.h"
#include "generated/scoriadb.grpc.pb.h"

// Authentication request
scoriadb::AuthRequest auth_request;
auth_request.set_username("admin");
auth_request.set_password("admin");

// Call Authenticate
scoriadb::AuthResponse auth_response;
grpc::ClientContext context;
auto status = stub->Authenticate(&context, auth_request, &auth_response);

if (status.ok()) {
    std::string token = auth_response.jwt_token();
    
    // Create metadata with token for subsequent calls
    // We'll attach it to each call via ClientContext
} else {
    std::cerr << "Authentication failed: " << status.error_message() << std::endl;
}
```

| Field | Type | Description |
|-------|------|-------------|
| `username` | `string` | User name (default: `admin` on first start) |
| `password` | `string` | User password (default: `admin` on first start) |
| `jwt_token()` | `string` | Token to use in `authorization` header for all future calls |

---

## Helper Function for Metadata

To simplify adding the token to each request, create a helper function:

```cpp
grpc::ClientContext CreateContextWithToken(const std::string& token) {
    grpc::ClientContext context;
    context.AddMetadata("authorization", "Bearer " + token);
    return context;
}
```

---

## Write a Key (Put)

**What this does:**  
Stores a key-value pair in the database. If the key already exists, it overwrites the value (MVCC creates a new version).

**When to use:**  
Every time you need to save or update data.

```cpp
void Put(const std::string& token, const std::string& key, 
         const std::string& value, const std::string& cf = "default") {
    
    scoriadb::PutRequest request;
    request.set_key(key);
    request.set_value(value);
    request.set_cf_name(cf);
    
    scoriadb::PutResponse response;
    auto context = CreateContextWithToken(token);
    
    auto status = stub->Put(&context, request, &response);
    if (!status.ok()) {
        throw std::runtime_error("Put failed: " + status.error_message());
    }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `key` | `string` | Yes | Unique identifier |
| `value` | `string` | Yes | Data to store. Can be text, JSON, binary (use `std::string` with binary data) |
| `cf_name` | `string` | No | Column Family (default: `"default"`). Use different CFs to separate data types |

---

## Read a Key (Get)

**What this does:**  
Retrieves the value for a given key. Returns the latest version (highest commit timestamp).

**When to use:**  
Whenever you need to fetch data by its exact key.

```cpp
std::optional<std::string> Get(const std::string& token, const std::string& key,
                                const std::string& cf = "default") {
    
    scoriadb::GetRequest request;
    request.set_key(key);
    request.set_cf_name(cf);
    
    scoriadb::GetResponse response;
    auto context = CreateContextWithToken(token);
    
    auto status = stub->Get(&context, request, &response);
    if (!status.ok()) {
        throw std::runtime_error("Get failed: " + status.error_message());
    }
    
    if (response.found()) {
        return response.value();
    }
    return std::nullopt;  // key not found
}
```

| Field | Type | Description |
|-------|------|-------------|
| `found()` | `bool` | `true` if the key exists, `false` otherwise |
| `value()` | `string` | Stored value (if found) |

---

## Delete a Key (Delete)

**What this does:**  
Removes a key from the database. Actually creates a "tombstone" version – the key is no longer visible but the old version may still exist until compaction runs.

**When to use:**  
When you no longer need a piece of data.

```cpp
void Delete(const std::string& token, const std::string& key,
            const std::string& cf = "default") {
    
    scoriadb::DeleteRequest request;
    request.set_key(key);
    request.set_cf_name(cf);
    
    scoriadb::DeleteResponse response;
    auto context = CreateContextWithToken(token);
    
    auto status = stub->Delete(&context, request, &response);
    if (!status.ok()) {
        throw std::runtime_error("Delete failed: " + status.error_message());
    }
}
```

---

## Scan Keys by Prefix (Scan)

**What this does:**  
Finds all keys that start with a given prefix. Returns them as a stream (one message per key-value pair). Useful for iteration without loading everything into memory at once.

**When to use:**
- You need to find multiple keys (e.g., all users with prefix `user:`)
- You don't know the exact key names
- You are building a search or list feature

```cpp
void Scan(const std::string& token, const std::string& prefix,
          const std::string& cf = "default") {
    
    scoriadb::ScanRequest request;
    request.set_prefix(prefix);
    request.set_cf_name(cf);
    
    auto context = CreateContextWithToken(token);
    auto reader = stub->Scan(&context, request);
    
    scoriadb::ScanResponse response;
    while (reader->Read(&response)) {
        std::cout << "  " << response.key() << " → " << response.value() << std::endl;
    }
    
    auto status = reader->Finish();
    if (!status.ok()) {
        throw std::runtime_error("Scan failed: " + status.error_message());
    }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `prefix` | `string` | Only keys starting with this prefix. Empty `""` returns all keys |
| `cf_name` | `string` | Column Family (default: `"default"`) |

**How streaming works:**  
The server sends results one by one. The `reader->Read()` loop receives each key-value pair as it becomes available. This is memory-efficient for large result sets.

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

```cpp
std::string BeginTransaction(const std::string& token) {
    scoriadb::BeginTxnRequest request;
    scoriadb::BeginTxnResponse response;
    auto context = CreateContextWithToken(token);
    
    auto status = stub->BeginTxn(&context, request, &response);
    if (!status.ok()) {
        throw std::runtime_error("BeginTxn failed: " + status.error_message());
    }
    
    return response.txn_id();
}
```

| Field | Type | Description |
|-------|------|-------------|
| `txn_id()` | `string` | Unique transaction identifier. Use this in `CommitTxn` |
| `start_ts()` | `int64` | Snapshot timestamp for this transaction (for debugging) |

### Step 2: Commit Transaction

**What this does:**  
Applies all operations atomically. If any key was modified by another transaction after `BeginTxn`, the commit fails with `ABORTED` status.

**When to retry:**  
If you get `ABORTED`, wait a short time (e.g., 10ms) and retry the entire transaction from `BeginTxn`.

```cpp
void CommitTransaction(const std::string& token, const std::string& txn_id,
                       const std::vector<std::pair<std::string, std::string>>& puts = {},
                       const std::vector<std::string>& deletes = {}) {
    
    scoriadb::CommitTxnRequest request;
    request.set_txn_id(txn_id);
    
    // Add PUT operations
    for (const auto& [key, value] : puts) {
        auto* op = request.add_ops();
        op->set_op(scoriadb::TxnOp_OpType_PUT);
        op->set_key(key);
        op->set_value(value);
    }
    
    // Add DELETE operations
    for (const auto& key : deletes) {
        auto* op = request.add_ops();
        op->set_op(scoriadb::TxnOp_OpType_DELETE);
        op->set_key(key);
    }
    
    scoriadb::CommitTxnResponse response;
    auto context = CreateContextWithToken(token);
    
    auto status = stub->CommitTxn(&context, request, &response);
    if (!status.ok()) {
        throw std::runtime_error("CommitTxn failed: " + status.error_message());
    }
}
```

---

## Full C++ Client Example

```cpp
#include <iostream>
#include <memory>
#include <optional>
#include <vector>
#include <stdexcept>
#include <thread>
#include <chrono>
#include <grpcpp/grpcpp.h>
#include "generated/scoriadb.grpc.pb.h"

class ScoriaDBClient {
private:
    std::unique_ptr<scoriadb::ScoriaDB::Stub> stub;
    std::string token;

    grpc::ClientContext CreateContext() {
        grpc::ClientContext context;
        context.AddMetadata("authorization", "Bearer " + token);
        return context;
    }

public:
    ScoriaDBClient(const std::string& address = "localhost:50051") {
        auto channel = grpc::CreateChannel(address, grpc::InsecureChannelCredentials());
        stub = scoriadb::ScoriaDB::NewStub(channel);
    }

    void Login(const std::string& username, const std::string& password) {
        scoriadb::AuthRequest request;
        request.set_username(username);
        request.set_password(password);
        
        scoriadb::AuthResponse response;
        grpc::ClientContext context;
        
        auto status = stub->Authenticate(&context, request, &response);
        if (!status.ok()) {
            throw std::runtime_error("Login failed: " + status.error_message());
        }
        
        token = response.jwt_token();
        std::cout << "✅ Authenticated as " << username << std::endl;
    }

    void Put(const std::string& key, const std::string& value, const std::string& cf = "default") {
        scoriadb::PutRequest request;
        request.set_key(key);
        request.set_value(value);
        request.set_cf_name(cf);
        
        scoriadb::PutResponse response;
        auto context = CreateContext();
        
        auto status = stub->Put(&context, request, &response);
        if (!status.ok()) {
            throw std::runtime_error("Put failed: " + status.error_message());
        }
        std::cout << "✅ Put: " << key << " = " << value << std::endl;
    }

    std::optional<std::string> Get(const std::string& key, const std::string& cf = "default") {
        scoriadb::GetRequest request;
        request.set_key(key);
        request.set_cf_name(cf);
        
        scoriadb::GetResponse response;
        auto context = CreateContext();
        
        auto status = stub->Get(&context, request, &response);
        if (!status.ok()) {
            throw std::runtime_error("Get failed: " + status.error_message());
        }
        
        if (response.found()) {
            return response.value();
        }
        return std::nullopt;
    }

    void Delete(const std::string& key, const std::string& cf = "default") {
        scoriadb::DeleteRequest request;
        request.set_key(key);
        request.set_cf_name(cf);
        
        scoriadb::DeleteResponse response;
        auto context = CreateContext();
        
        auto status = stub->Delete(&context, request, &response);
        if (!status.ok()) {
            throw std::runtime_error("Delete failed: " + status.error_message());
        }
        std::cout << "✅ Deleted: " << key << std::endl;
    }

    void Scan(const std::string& prefix = "", const std::string& cf = "default") {
        scoriadb::ScanRequest request;
        request.set_prefix(prefix);
        request.set_cf_name(cf);
        
        auto context = CreateContext();
        auto reader = stub->Scan(&context, request);
        
        scoriadb::ScanResponse response;
        int count = 0;
        while (reader->Read(&response)) {
            std::cout << "  " << response.key() << " → " << response.value() << std::endl;
            count++;
        }
        
        auto status = reader->Finish();
        if (!status.ok()) {
            throw std::runtime_error("Scan failed: " + status.error_message());
        }
        std::cout << "Total: " << count << " keys" << std::endl;
    }

    std::string BeginTransaction() {
        scoriadb::BeginTxnRequest request;
        scoriadb::BeginTxnResponse response;
        auto context = CreateContext();
        
        auto status = stub->BeginTxn(&context, request, &response);
        if (!status.ok()) {
            throw std::runtime_error("BeginTransaction failed: " + status.error_message());
        }
        std::cout << "📖 Transaction started: " << response.txn_id() << std::endl;
        return response.txn_id();
    }

    void CommitTransaction(const std::string& txn_id,
                           const std::vector<std::pair<std::string, std::string>>& puts = {},
                           const std::vector<std::string>& deletes = {}) {
        scoriadb::CommitTxnRequest request;
        request.set_txn_id(txn_id);
        
        for (const auto& [key, value] : puts) {
            auto* op = request.add_ops();
            op->set_op(scoriadb::TxnOp_OpType_PUT);
            op->set_key(key);
            op->set_value(value);
        }
        
        for (const auto& key : deletes) {
            auto* op = request.add_ops();
            op->set_op(scoriadb::TxnOp_OpType_DELETE);
            op->set_key(key);
        }
        
        scoriadb::CommitTxnResponse response;
        auto context = CreateContext();
        
        auto status = stub->CommitTxn(&context, request, &response);
        if (!status.ok()) {
            throw std::runtime_error("CommitTransaction failed: " + status.error_message());
        }
        std::cout << "✅ Transaction " << txn_id << " committed" << std::endl;
    }
};

// ============================================
// Usage Example with Retry Logic
// ============================================

void CommitWithRetry(ScoriaDBClient& client, const std::string& txn_id,
                     const std::vector<std::pair<std::string, std::string>>& puts,
                     const std::vector<std::string>& deletes,
                     int max_retries = 3) {
    for (int attempt = 0; attempt < max_retries; attempt++) {
        try {
            client.CommitTransaction(txn_id, puts, deletes);
            return;
        } catch (const std::runtime_error& e) {
            std::string err = e.what();
            if (err.find("ABORTED") != std::string::npos && attempt < max_retries - 1) {
                std::cout << "Conflict, retrying (" << attempt + 1 << "/" << max_retries << ")" << std::endl;
                std::this_thread::sleep_for(std::chrono::milliseconds(10 * (1 << attempt)));
                continue;
            }
            throw;
        }
    }
}

int main() {
    try {
        // Create client (connects immediately)
        ScoriaDBClient client("localhost:50051");

        // Authenticate (admin/admin on first start)
        client.Login("admin", "admin");
        std::cout << "✅ Connected and authenticated" << std::endl;

        // Write data
        client.Put("user:1", "Alice");
        client.Put("user:2", "Bob");
        std::cout << "✅ Wrote user:1 and user:2" << std::endl;

        // Read data
        auto user1 = client.Get("user:1");
        if (user1.has_value()) {
            std::cout << "📖 user:1 = " << user1.value() << std::endl;
        }

        // Delete a key
        client.Delete("user:2");
        std::cout << "✅ Deleted user:2" << std::endl;

        // Scan all keys
        std::cout << "\n📖 Scan results:" << std::endl;
        client.Scan("");

        // Transaction example
        std::string txn_id = client.BeginTransaction();
        CommitWithRetry(client, txn_id, 
                       {{"txn_key", "txn_value"}},  // puts
                       {"temp_key"});                // deletes

        // Verify transaction result
        auto txn_val = client.Get("txn_key");
        if (txn_val.has_value()) {
            std::cout << "📖 After transaction: txn_key = " << txn_val.value() << std::endl;
        }

    } catch (const std::exception& e) {
        std::cerr << "Error: " << e.what() << std::endl;
        return 1;
    }

    return 0;
}
```

---

## Compiling the Example

```bash
# Create build directory
mkdir -p build
cd build

# Configure with CMake
cmake ..

# Build
make

# Run the client
./scoria_client
```

---

## Error Handling

**What are gRPC errors?**  
When something goes wrong, gRPC returns a `Status` object with an error code and message. Your code should check the status and react appropriately.

**When to use error handling:**  
Always check the status after each gRPC call, especially in production.

Common status codes:

| Code | Description | How to handle |
|------|-------------|---------------|
| `UNAUTHENTICATED` | Invalid or missing token | Check credentials or token; re-authenticate |
| `PERMISSION_DENIED` | Role does not allow this operation | Use a user with higher privileges |
| `NOT_FOUND` | Key, CF, or user does not exist | Verify the name before using it |
| `ABORTED` | Transaction conflict | Retry the entire transaction (wait 10-100ms first) |
| `UNAVAILABLE` | Server not reachable | Check if server is running; retry later |
| `DEADLINE_EXCEEDED` | Operation took too long | Increase timeout or reduce data size |
| `INTERNAL` | Server-side error | Check server logs; report bug |

Example of checking status codes:

```cpp
auto status = stub->Put(&context, request, &response);
switch (status.error_code()) {
    case grpc::StatusCode::UNAVAILABLE:
        std::cerr << "Server is not running" << std::endl;
        break;
    case grpc::StatusCode::PERMISSION_DENIED:
        std::cerr << "Insufficient permissions" << std::endl;
        break;
    case grpc::StatusCode::ABORTED:
        std::cerr << "Transaction conflict, retry" << std::endl;
        break;
    default:
        if (!status.ok()) {
            std::cerr << "Error: " << status.error_message() << std::endl;
        }
}
```

---

## Method Reference

| Method | C++ call | When to use |
|--------|----------|-------------|
| `Authenticate` | `stub->Authenticate()` | Once at startup, before any other call |
| `Put` | `stub->Put()` | Every time you need to save or update data |
| `Get` | `stub->Get()` | When you know the exact key |
| `Delete` | `stub->Delete()` | When a key is no longer needed |
| `Scan` | `stub->Scan()` | When you need to find keys by prefix |
| `BeginTxn` | `stub->BeginTxn()` | Before a series of operations that must be atomic |
| `CommitTxn` | `stub->CommitTxn()` | After preparing operations for a transaction |

---

## Summary

| Step | What | Why |
|------|------|-----|
| 1 | Install gRPC and protobuf | Required libraries |
| 2 | Generate C++ code from `.proto` | Create client classes |
| 3 | Create `Channel` and `Stub` | Establish network connection |
| 4 | Call `Authenticate` | Get JWT token |
| 5 | Add token to `ClientContext` metadata | Authorize all subsequent calls |
| 6 | Use `Put`/`Get`/`Delete`/`Scan` | Work with data |
| 7 | Use `BeginTxn` + `CommitTxn` | Group operations atomically |
| 8 | Check `Status` | Handle failures gracefully |

The C++ client is best suited for:

- Game engines (Unreal Engine, custom engines)
- Real-time systems
- High-performance computing applications
- Existing C++ codebases that need persistent storage
- Embedded systems with network capabilities

For maximum performance, use the Go embedded API instead. For other languages, the same gRPC pattern applies.