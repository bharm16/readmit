package org.readmit.fhir;

import java.io.IOException;
import java.net.URISyntaxException;
import java.util.List;
import org.hl7.fhir.r5.renderers.RendererFactory;
import org.hl7.fhir.r5.terminologies.client.TerminologyClientContext;
import org.hl7.fhir.validation.IgLoader;
import org.hl7.fhir.validation.ValidationEngine;
import org.hl7.fhir.validation.cli.picocli.CLI;
import org.hl7.fhir.validation.service.ValidationService;

/**
 * A fixed package-loader adapter for official validator6.10.4. The upstream
 * CLI, parsers, evaluator and OperationOutcome writer remain unchanged. The
 * only substituted behavior is its two unversioned default IG lookups.
 */
public final class OfflineValidator extends ValidationService {
  public OfflineValidator() {
    super(new RendererFactory());
  }
  @Override
  protected void loadIgsAndExtensions(
      ValidationEngine engine, List<String> igs, boolean recursive)
      throws IOException, URISyntaxException {
    if (!"4.0.1".equals(engine.getVersion())) {
      throw new IOException("This staged capability supports FHIR R4 4.0.1 only");
    }
    IgLoader loader = new IgLoader(
        engine.getPcm(), engine.getContext(), engine.getVersion(), engine.isDebug());
    loader.loadIg(engine.getIgs(), engine.getBinaries(), "hl7.terminology.r4#6.2.0", false);
    loader.loadIg(engine.getIgs(), engine.getBinaries(), "hl7.fhir.uv.extensions.r4#5.2.0", false);
    for (String source : igs) {
      loader.loadIg(engine.getIgs(), engine.getBinaries(), source, recursive);
    }
  }

  public static void main(String[] args) {
    System.setProperty("slf4j.internal.verbosity", "WARN");
    TerminologyClientContext.setCanUseCacheId(true);
    System.exit(new CLI(new OfflineValidator()).parseArgsAndExecuteCommand(args));
  }
}
