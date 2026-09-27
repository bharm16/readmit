package lab;

import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.URI;
import java.nio.charset.StandardCharsets;

/** Transport only. All resource transformation remains in the OIE channel. */
public final class LabHttp {
    public static String request(String address, String method, String body, String contentType, String access) throws Exception {
        URI uri = URI.create(address);
        if (!"https".equals(uri.getScheme()) || !"fixture".equals(uri.getHost()) || uri.getPort() != 9443 || uri.getUserInfo() != null || uri.getFragment() != null) {
            throw new IOException("outside independent lab target");
        }
        HttpURLConnection connection = (HttpURLConnection) uri.toURL().openConnection(java.net.Proxy.NO_PROXY);
        connection.setInstanceFollowRedirects(false);
        connection.setRequestMethod(method);
        connection.setConnectTimeout(5000);
        connection.setReadTimeout(20000);
        connection.setRequestProperty("Content-Type", contentType);
        if (access != null) connection.setRequestProperty("Authorization", "Bearer " + access);
        try {
            if (body != null) {
                byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
                if (bytes.length > 1048576) throw new IOException("lab request exceeds bound");
                connection.setDoOutput(true);
                try (var output = connection.getOutputStream()) { output.write(bytes); }
            }
            int status = connection.getResponseCode();
            if (status < 200 || status >= 300) throw new IOException("independent lab target refused status " + status);
            try (var input = connection.getInputStream()) {
                byte[] bytes = input.readNBytes(8388609);
                if (bytes.length > 8388608) throw new IOException("lab response exceeds bound");
                return new String(bytes, StandardCharsets.UTF_8);
            }
        } finally { connection.disconnect(); }
    }
}
