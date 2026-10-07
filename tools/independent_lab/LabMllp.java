package lab;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.security.KeyStore;
import java.security.MessageDigest;
import java.security.cert.CertificateFactory;
import java.time.Instant;
import java.util.Base64;
import java.util.HexFormat;
import java.util.UUID;
import javax.net.ssl.KeyManager;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocket;
import javax.net.ssl.TrustManagerFactory;

/** Independent sender transport and acquisition only; no expected-field oracle. */
public final class LabMllp {
    private static String hash(byte[] bytes) throws Exception {
        return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(bytes));
    }
    private static String quote(String text) {
        return "\"" + text.replace("\\", "\\\\").replace("\"", "\\\"").replace("\n", "\\n").replace("\r", "\\r") + "\"";
    }
    public static String transmit(String message, String generation, String transport, String host, int port) throws Exception {
        if (!generation.matches("lab-[a-z0-9-]{1,48}") || !host.matches("[0-9.]{7,15}") || port < 1024 || port > 65535 || !java.util.Set.of("plain", "tls", "mutual-tls").contains(transport)) {
            throw new IOException("invalid owned return declaration");
        }
        byte[] intended=("\u000b" + message + "\u001c\r").getBytes(StandardCharsets.UTF_8);
        if (intended.length > 1048576) throw new IOException("return message exceeds bound");
        String id=UUID.randomUUID().toString();
        Path directory=Path.of("/session-evidence",generation,"return-receipts");
        Files.createDirectories(directory);
        Path receipt=directory.resolve(id+".json");
        String started=Instant.now().toString();
        Files.writeString(directory.resolve(id+"-intent.json"),"{\"schema\":\"readmit-lab-return-intent/v1\",\"generation\":"+quote(generation)+",\"started_at\":"+quote(started)+",\"intended_sha256\":"+quote(hash(intended))+"}");
        String peer="", client="", protocol="", cipher="", writeStarted="", sentAt="", acquiredAt="", state="refused", error="";
        boolean written=false;
        ByteArrayOutputStream ack=new ByteArrayOutputStream();
        Socket socket=null;
        try {
            socket=new Socket();socket.connect(new java.net.InetSocketAddress("fixture",7001),10000);socket.setSoTimeout(15000);
            if (!transport.equals("plain")) {
                KeyStore trust=KeyStore.getInstance(KeyStore.getDefaultType());trust.load(null,null);
                try (var input=Files.newInputStream(Path.of("/run/lab/ca.pem"))) {trust.setCertificateEntry("lab",CertificateFactory.getInstance("X.509").generateCertificate(input));}
                TrustManagerFactory tm=TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());tm.init(trust);
                KeyManager[] keys=null;
                if (transport.equals("mutual-tls")) {
                    char[] password=Files.readString(Path.of("/run/lab/keystore-password")).toCharArray();
                    KeyStore own=KeyStore.getInstance("PKCS12");
                    try(var input=Files.newInputStream(Path.of("/run/lab/return-client.p12"))) {own.load(input,password);}
                    KeyManagerFactory km=KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());km.init(own,password);keys=km.getKeyManagers();
                    java.util.Arrays.fill(password,'\0');
                }
                SSLContext tls=SSLContext.getInstance("TLS");tls.init(keys,tm.getTrustManagers(),null);
                SSLSocket secured=(SSLSocket)tls.getSocketFactory().createSocket(socket,"localhost",7001,true);
                socket=secured;secured.setSoTimeout(15000);secured.setEnabledProtocols(new String[]{"TLSv1.3","TLSv1.2"});
                var parameters=secured.getSSLParameters();parameters.setEndpointIdentificationAlgorithm("HTTPS");secured.setSSLParameters(parameters);
                secured.startHandshake();
                var session=secured.getSession();peer=hash(session.getPeerCertificates()[0].getEncoded());
                if (session.getLocalCertificates()!=null) client=hash(session.getLocalCertificates()[0].getEncoded());
                protocol=session.getProtocol();cipher=session.getCipherSuite();
            }
            state="uncertain";writeStarted=Instant.now().toString();
            socket.getOutputStream().write(intended);socket.getOutputStream().flush();written=true;sentAt=Instant.now().toString();
            int previous=-1;
            while (ack.size()<65536) {
                int value=socket.getInputStream().read();
                if(value<0)throw new IOException("incomplete return acknowledgement");
                ack.write(value);
                if(previous==28 && value==13)break;
                previous=value;
            }
            byte[] acquired=ack.toByteArray();acquiredAt=Instant.now().toString();
            if(acquired.length<3 || acquired[0]!=11 || acquired[acquired.length-2]!=28 || acquired[acquired.length-1]!=13)throw new IOException("incomplete return acknowledgement");
            String control=message.split("\\r",2)[0].split("\\|",-1)[9];
            boolean accepted=false;
            for(String line:new String(acquired,StandardCharsets.UTF_8).split("\\r")) {
                String[] fields=line.split("\\|",-1);
                if(fields.length>=3 && fields[0].equals("MSA") && fields[1].equals("AA") && fields[2].equals(control))accepted=true;
            }
            if(!accepted)throw new IOException("return acknowledgement is not correlated AA");
            state="acknowledged";
            return "independent product return acknowledged";
        } catch(Exception failure) {
            error=failure.getClass().getSimpleName();throw failure;
        } finally {
            if(socket!=null)try{socket.close();}catch(IOException ignored){}
            String encoded=Base64.getEncoder().encodeToString(intended);
            String body="{\"schema\":\"readmit-lab-return-receipt/v1\",\"generation\":"+quote(generation)+",\"transport\":"+quote(transport)+",\"server_name\":\"localhost\",\"target_host\":"+quote(host)+",\"target_port\":"+port+",\"state\":"+quote(state)+",\"started_at\":"+quote(started)+",\"write_started_at\":"+quote(writeStarted)+",\"sent_at\":"+quote(sentAt)+",\"ack_received_at\":"+quote(acquiredAt)+",\"intended_base64\":"+quote(encoded)+",\"transmitted_base64\":"+quote(written?encoded:"")+",\"ack_base64\":"+quote(Base64.getEncoder().encodeToString(ack.toByteArray()))+",\"peer_certificate_sha256\":"+quote(peer)+",\"client_certificate_sha256\":"+quote(client)+",\"tls_protocol\":"+quote(protocol)+",\"cipher_suite\":"+quote(cipher)+",\"error_class\":"+quote(error)+"}";
            Path temporary=directory.resolve(id+".incomplete");Files.writeString(temporary,body);Files.move(temporary,receipt,StandardCopyOption.ATOMIC_MOVE);
        }
    }
}
