
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