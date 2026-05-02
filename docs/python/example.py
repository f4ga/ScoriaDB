import grpc  # type: ignore
import scoriadb_pb2  # type: ignore
import scoriadb_pb2_grpc  # type: ignore
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
        request = scoriadb_pb2.AuthRequest(username=username, password=password)
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
        return (("authorization", f"Bearer {self.token}"),)

    def put(self, key, value, cf="default"):
        """Store a key-value pair"""
        request = scoriadb_pb2.PutRequest(
            key=key.encode("utf-8"), value=value.encode("utf-8"), cf_name=cf
        )
        self.stub.Put(request, metadata=self._metadata())

    def get(self, key, cf="default"):
        """Retrieve value for a key. Returns None if not found."""
        request = scoriadb_pb2.GetRequest(key=key.encode("utf-8"), cf_name=cf)
        response = self.stub.Get(request, metadata=self._metadata())
        return response.value.decode("utf-8") if response.found else None

    def delete(self, key, cf="default"):
        """Delete a key"""
        request = scoriadb_pb2.DeleteRequest(key=key.encode("utf-8"), cf_name=cf)
        self.stub.Delete(request, metadata=self._metadata())

    def scan(self, prefix="", cf="default"):
        """Scan keys with given prefix. Returns list of (key, value) tuples."""
        request = scoriadb_pb2.ScanRequest(prefix=prefix.encode("utf-8"), cf_name=cf)
        results = []
        for response in self.stub.Scan(request, metadata=self._metadata()):
            key = response.key.decode("utf-8")
            value = response.value.decode("utf-8")
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
            if op[0] == "put":
                _, key, value = op
                txn_op = scoriadb_pb2.TxnOp(
                    op=scoriadb_pb2.TxnOp.PUT,
                    key=key.encode("utf-8"),
                    value=value.encode("utf-8"),
                )
            elif op[0] == "delete":
                _, key = op
                txn_op = scoriadb_pb2.TxnOp(
                    op=scoriadb_pb2.TxnOp.DELETE, key=key.encode("utf-8")
                )
            else:
                continue
            ops.append(txn_op)

        request = scoriadb_pb2.CommitTxnRequest(txn_id=txn_id, ops=ops)
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

    client.commit_transaction(
        txn_id, [("put", "txn_key", "txn_value"), ("delete", "temp_key")]
    )
    print("✅ Transaction committed")

    # Clean up
    client.close()


if __name__ == "__main__":
    main()
