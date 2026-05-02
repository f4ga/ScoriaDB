package com.example;

import com.google.protobuf.ByteString;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Metadata;
import io.grpc.StatusRuntimeException;
import io.grpc.stub.MetadataUtils;
import scoriadb.*;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.List;

public class ScoriaDBClient {
    private ManagedChannel channel;
    private ScoriaDBGrpc.ScoriaDBBlockingStub stub;
    private String token;

    public ScoriaDBClient() {
        this("localhost", 50051);
    }

    public ScoriaDBClient(String host, int port) {
        this.channel = ManagedChannelBuilder.forAddress(host, port)
                .usePlaintext()
                .build();
        this.stub = ScoriaDBGrpc.newBlockingStub(channel);
    }

    public void login(String username, String password) {
        AuthRequest request = AuthRequest.newBuilder()
                .setUsername(username)
                .setPassword(password)
                .build();

        try {
            AuthResponse response = stub.authenticate(request);
            this.token = response.getJwtToken();

            Metadata metadata = new Metadata();
            Metadata.Key<String> authKey = Metadata.Key.of("authorization",
                    Metadata.ASCII_STRING_MARSHALLER);
            metadata.put(authKey, "Bearer " + token);
            this.stub = stub.withInterceptors(MetadataUtils.newAttachHeadersInterceptor(metadata));

            System.out.println("Authenticated as " + username);
        } catch (StatusRuntimeException e) {
            System.err.println("Authentication failed: " + e.getStatus());
            throw e;
        }
    }

    public void put(String key, String value, String cf) {
        PutRequest request = PutRequest.newBuilder()
                .setKey(ByteString.copyFromUtf8(key))
                .setValue(ByteString.copyFromUtf8(value))
                .setCfName(cf)
                .build();
        stub.put(request);
        System.out.println("Put: " + key + " = " + value);
    }

    public void put(String key, String value) {
        put(key, value, "default");
    }

    public String get(String key, String cf) {
        GetRequest request = GetRequest.newBuilder()
                .setKey(ByteString.copyFromUtf8(key))
                .setCfName(cf)
                .build();
        GetResponse response = stub.get(request);
        return response.getFound() ? response.getValue().toStringUtf8() : null;
    }

    public String get(String key) {
        return get(key, "default");
    }

    public void delete(String key, String cf) {
        DeleteRequest request = DeleteRequest.newBuilder()
                .setKey(ByteString.copyFromUtf8(key))
                .setCfName(cf)
                .build();
        stub.delete(request);
        System.out.println("Deleted: " + key);
    }

    public void delete(String key) {
        delete(key, "default");
    }

    public List<KeyValue> scan(String prefix, String cf) {
        ScanRequest request = ScanRequest.newBuilder()
                .setPrefix(ByteString.copyFromUtf8(prefix))
                .setCfName(cf)
                .build();

        List<KeyValue> results = new ArrayList<>();
        Iterator<ScanResponse> iterator = stub.scan(request);

        while (iterator.hasNext()) {
            ScanResponse response = iterator.next();
            results.add(new KeyValue(response.getKey().toStringUtf8(),
                    response.getValue().toStringUtf8()));
        }
        return results;
    }

    public List<KeyValue> scan(String prefix) {
        return scan(prefix, "default");
    }

    public String beginTransaction() {
        BeginTxnRequest request = BeginTxnRequest.newBuilder().build();
        BeginTxnResponse response = stub.beginTxn(request);
        return response.getTxnId();
    }

    public void commitPutTransaction(String txnId, String key, String value, String cf) {
        List<TxnOp> ops = new ArrayList<>();
        TxnOp op = TxnOp.newBuilder()
                .setOp(TxnOp.OpType.PUT)
                .setKey(ByteString.copyFromUtf8(key))
                .setValue(ByteString.copyFromUtf8(value))
                .setCfName(cf)
                .build();
        ops.add(op);

        CommitTxnRequest request = CommitTxnRequest.newBuilder()
                .setTxnId(txnId)
                .addAllOps(ops)
                .build();
        stub.commitTxn(request);
        System.out.println("Transaction " + txnId + " committed (PUT)");
    }

    public void commitDeleteTransaction(String txnId, String key, String cf) {
        List<TxnOp> ops = new ArrayList<>();
        TxnOp op = TxnOp.newBuilder()
                .setOp(TxnOp.OpType.DELETE)
                .setKey(ByteString.copyFromUtf8(key))
                .setCfName(cf)
                .build();
        ops.add(op);

        CommitTxnRequest request = CommitTxnRequest.newBuilder()
                .setTxnId(txnId)
                .addAllOps(ops)
                .build();
        stub.commitTxn(request);
        System.out.println("Transaction " + txnId + " committed (DELETE)");
    }

    public void close() {
        if (channel != null) {
            channel.shutdown();
        }
    }

    public static class KeyValue {
        public final String key;
        public final String value;

        public KeyValue(String key, String value) {
            this.key = key;
            this.value = value;
        }
    }

    public static void main(String[] args) {
        ScoriaDBClient client = new ScoriaDBClient("localhost", 50051);

        try {
            client.login("admin", "admin");
            System.out.println("Connected and authenticated");

            client.put("user:1", "Alice");
            client.put("user:2", "Bob");

            String user1 = client.get("user:1");
            System.out.println("user:1 = " + user1);

            client.delete("user:2");

            List<KeyValue> allKeys = client.scan("");
            System.out.println("Total keys: " + allKeys.size());
            for (KeyValue kv : allKeys) {
                System.out.println("  " + kv.key + " -> " + kv.value);
            }

            String txnId = client.beginTransaction();
            System.out.println("Transaction started: " + txnId);
            client.commitPutTransaction(txnId, "txn_key", "txn_value", "default");

        } catch (StatusRuntimeException e) {
            System.err.println("gRPC error: " + e.getStatus());
            e.printStackTrace();
        } finally {
            client.close();
        }
    }
}