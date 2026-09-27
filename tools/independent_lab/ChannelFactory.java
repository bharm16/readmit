import java.nio.file.Files;
import java.nio.file.Path;
import com.mirth.connect.model.*;
import com.mirth.connect.model.converters.ObjectXMLSerializer;
import com.mirth.connect.connectors.tcp.TcpReceiverProperties;
import com.mirth.connect.connectors.js.JavaScriptDispatcherProperties;
import com.mirth.connect.plugins.datatypes.hl7v2.HL7v2DataTypeProperties;
import com.mirth.connect.donkey.model.channel.SourceConnectorProperties;

/** Uses the pinned engine's own model and serializer to build lab channels. */
public final class ChannelFactory {
    private static Transformer transformer() {
        Transformer t = new Transformer();
        t.setInboundDataType("HL7V2");
        t.setOutboundDataType("HL7V2");
        t.setInboundProperties(new HL7v2DataTypeProperties());
        t.setOutboundProperties(new HL7v2DataTypeProperties());
        return t;
    }
    public static void main(String[] args) throws Exception {
        if (ObjectXMLSerializer.getInstance().getNormalizedVersion() == null) ObjectXMLSerializer.getInstance().init("4.6.0");
        if (args.length == 2 && args[0].equals("--all")) {
            try (var paths = Files.list(Path.of(args[1]))) {
                for (Path file : paths.filter(p -> p.toString().endsWith(".js")).sorted().toList()) {
                    String name = file.getFileName().toString();
                    boolean v2 = name.startsWith("v2v2-");
                    if (!v2 && !name.startsWith("v2fhir-")) throw new IllegalArgumentException("unknown lab route");
                    main(new String[]{v2 ? "10000000-0000-0000-0000-000000000001" : "10000000-0000-0000-0000-000000000002", name, v2 ? "6661" : "6662", file.toString(), file.toString().replaceAll("\\.js$", ".xml")});
                }
            }
            return;
        }
        Channel channel = new Channel();
        channel.setId(args[0]);
        channel.setName(args[1]);
        channel.setDescription("Wholly synthetic isolated reference target; not EHR certification");
        channel.setRevision(1);
        channel.setPreprocessingScript("return message;");
        channel.setPostprocessingScript("return;");
        channel.setDeployScript("globalChannelMap.put('lab-scheduler', java.util.concurrent.Executors.newSingleThreadScheduledExecutor());");
        channel.setUndeployScript("var executor=globalChannelMap.get('lab-scheduler'); if(executor){executor.shutdownNow();executor.awaitTermination(5,java.util.concurrent.TimeUnit.SECONDS);}");
        Connector source = new Connector("Lab input");
        source.setMetaDataId(0);
        source.setMode(Connector.Mode.SOURCE);
        source.setEnabled(true);
        source.setTransportName(TcpReceiverProperties.NAME);
        TcpReceiverProperties tcp = new TcpReceiverProperties();
        tcp.getListenerConnectorProperties().setPort(args[2]);
        tcp.getSourceConnectorProperties().setResponseVariable(SourceConnectorProperties.RESPONSE_DESTINATIONS_COMPLETED);
        source.setProperties(tcp);
        source.setTransformer(transformer());
        source.setResponseTransformer(transformer());
        source.setFilter(new Filter());
        channel.setSourceConnector(source);
        Connector destination = new Connector("Independent target transformation");
        destination.setMetaDataId(1);
        destination.setMode(Connector.Mode.DESTINATION);
        destination.setEnabled(true);
        destination.setTransportName(JavaScriptDispatcherProperties.NAME);
        JavaScriptDispatcherProperties js = new JavaScriptDispatcherProperties();
        js.setScript(Files.readString(Path.of(args[3])));
        destination.setProperties(js);
        destination.setTransformer(transformer());
        destination.setResponseTransformer(transformer());
        destination.setFilter(new Filter());
        channel.addDestination(destination);
        channel.setNextMetaDataId(2);
        Files.writeString(Path.of(args[4]), ObjectXMLSerializer.getInstance().serialize(channel));
    }
}
