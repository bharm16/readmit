// The first line is an injected, versioned lab declaration, not an oracle.
var cfg = __LAB_CONFIGURATION__;
var raw = String(connectorMessage.getRawData());
var lines = raw.split(/\r\n|\r|\n/).filter(function(x) { return x.length > 0; });
function field(segment, number, occurrence) {
    var matching = lines.filter(function(x) { return x.substring(0, 3) === segment; });
    var parts = String(matching[(occurrence || 1) - 1] || '').split('|');
    return String(parts[segment === 'MSH' ? number - 1 : number] || '');
}
function component(value, n) { return String(value).split('^')[n - 1] || ''; }
function labID(value) { var id = component(value, 1); if (!/^lab-[a-z0-9-]+$/.test(id)) throw 'unowned fixture identifier'; return id; }
if (field('ZLG', 1) !== cfg.generation) throw 'old lab generation';
var bad = cfg.mode === 'defective' || cfg.mode === 'reintroduced';
var trigger = component(field('MSH', 9), 2);
function takeHeld() { var value=globalChannelMap.get('held');globalChannelMap.remove('held');if(value===null||value===undefined)throw 'missing held lab input';return String(value); }
function later(work) {
    var task = new java.lang.Runnable({run: function() { try { work(); } catch (ignored) {} }});
    globalChannelMap.get('lab-scheduler').schedule(task, 1200, java.util.concurrent.TimeUnit.MILLISECONDS);
}
function transmit(message) {
    var socket = new java.net.Socket('fixture', 7000);
    socket.setSoTimeout(5000);
    try {
        var output = socket.getOutputStream();
        output.write(new java.lang.String('\u000b' + message + '\u001c\r').getBytes('UTF-8'));
        output.flush();
        var input = socket.getInputStream();
        var previous = -1;var receipt=new java.io.ByteArrayOutputStream();
        for (var i = 0; i < 65536; i++) { var next = input.read(); if(next<0)break;receipt.write(next);if(previous===28&&next===13)break;previous=next; }
        if(String(new java.lang.String(receipt.toByteArray(),'UTF-8')).indexOf('MSA|AA|')<0)throw 'independent receiver did not acknowledge';
    } finally { socket.close(); }
}
function token() {
    var enc = java.util.Base64.getUrlEncoder().withoutPadding();
    function part(value) { return String(enc.encodeToString(new java.lang.String(JSON.stringify(value)).getBytes('UTF-8'))); }
    var now = Math.floor(new Date().getTime() / 1000);
    var signing = part({alg:'RS384',typ:'JWT'}) + '.' + part({iss:'engine-client',sub:'engine-client',aud:'https://fixture:9443/token',iat:now,exp:now+120,jti:String(java.util.UUID.randomUUID())});
    var pem = String(java.nio.file.Files.readString(java.nio.file.Paths.get('/run/lab/engine-client-key.pem'))).replace(/-----[^-]+-----/g,'').replace(/\s/g,'');
    var key = java.security.KeyFactory.getInstance('RSA').generatePrivate(new java.security.spec.PKCS8EncodedKeySpec(java.util.Base64.getDecoder().decode(pem)));
    var signature = java.security.Signature.getInstance('SHA384withRSA'); signature.initSign(key); signature.update(new java.lang.String(signing).getBytes('UTF-8'));
    var assertion = signing + '.' + enc.encodeToString(signature.sign());
    var body = 'grant_type=client_credentials&scope=system%2F*.crus&client_assertion_type=urn%3Aietf%3Aparams%3Aoauth%3Aclient-assertion-type%3Ajwt-bearer&client_assertion=' + assertion;
    return JSON.parse(http('https://fixture:9443/token','POST',body,'application/x-www-form-urlencoded',null)).access_token;
}
function http(url, method, body, contentType, access) {
    return String(Packages.lab.LabHttp.request(url,method,body,contentType,access));
}
function store(resource, duplicate) {
    resource.meta = {tag:[{system:'urn:readmit:independent-lab',code:cfg.generation}]};
    var path=resource.resourceType+'/'+resource.id;var method='PUT';
    if(duplicate){delete resource.id;path=resource.resourceType;method='POST';}
    http('https://fixture:9443/fhir/'+path,method,JSON.stringify(resource),'application/fhir+json',token());
}
if (cfg.route === 'v2v2') {
    var out=lines.slice();var header=out[0].split('|');header[2]='OIE-LAB';out[0]=header.join('|');
    if(bad&&trigger==='S13'&&cfg.defect==='field'){out=out.map(function(line){if(line.substring(0,3)!=='SCH')return line;var parts=line.split('|');parts[11]='^^^20300102094500+0000';return parts.join('|');});}
    if(bad&&trigger==='S13'&&cfg.defect==='link'){out=out.map(function(line){if(line.substring(0,3)!=='PID')return line;var parts=line.split('|');parts[3]='lab-wrong^LAB';return parts.join('|');});}
    var payload=out.join('\r')+'\r';
    if(bad&&cfg.defect==='reorder'){if(trigger==='S12'){globalChannelMap.put('held',payload);}else{transmit(payload);transmit(takeHeld());}}
    else if(!(bad&&trigger==='S13'&&cfg.defect==='drop')){transmit(payload);if(bad&&trigger==='S13'&&cfg.defect==='duplicate')transmit(payload);if(bad&&trigger==='S13'&&cfg.defect==='late-duplicate')later(function(){transmit(payload);});}
} else {
    var family=component(field('MSH',9),1);var patient=labID(field('PID',3));
    if(family==='ADT'){
        store({resourceType:'Patient',id:patient,identifier:[{system:'urn:readmit:lab:patient',value:patient}],name:[{family:component(field('PID',5),1),given:[component(field('PID',5),2)]}]},false);
        store({resourceType:'Encounter',id:labID(field('PV1',19)),status:trigger==='A03'?'finished':'in-progress',class:{system:'http://terminology.hl7.org/CodeSystem/v3-ActCode',code:'AMB'},subject:{reference:'Patient/'+patient}},false);
    } else if(family==='SIU'){
        var appointment=labID(field('SCH',1));
        var instant=java.time.OffsetDateTime.parse(component(field('SCH',11),4),java.time.format.DateTimeFormatter.ofPattern('yyyyMMddHHmmssXX')).toInstant();
        var resource={resourceType:'Appointment',id:appointment,identifier:[{system:'urn:readmit:lab:appointment',value:appointment}],status:'booked',start:String(instant.toString()),end:String(instant.plusSeconds(1800).toString()),participant:[{actor:{reference:'Patient/'+patient},status:'accepted'},{actor:{reference:'Practitioner/'+labID(field('AIP',3))},status:'accepted'},{actor:{reference:'Location/'+labID(field('AIL',3))},status:'accepted'}]};
        if(bad&&trigger==='S13'&&cfg.defect==='field'){resource.start='2030-01-02T09:45:00Z';resource.status='noshow';}
        if(bad&&trigger==='S13'&&cfg.defect==='link'){resource.participant[0].actor.reference='Patient/lab-wrong';}
        if(bad&&cfg.defect==='reorder'){if(trigger==='S12'){globalChannelMap.put('held',JSON.stringify(resource));}else{store(resource,false);store(JSON.parse(takeHeld()),false);}}
        else if(!(bad&&trigger==='S13'&&cfg.defect==='drop')){var copy=JSON.stringify(resource);store(resource,bad&&trigger==='S13'&&cfg.defect==='duplicate');if(bad&&trigger==='S13'&&cfg.defect==='late-duplicate')later(function(){store(JSON.parse(copy),true);});}
    } else if(family==='ORM'){
        store({resourceType:'ServiceRequest',id:labID(field('ORC',2)),status:field('ORC',1)==='CA'?'revoked':'active',intent:'order',identifier:[{system:'urn:readmit:lab:placer',value:component(field('ORC',2),1)},{system:'urn:readmit:lab:filler',value:component(field('ORC',3),1)}],subject:{reference:'Patient/'+patient},code:{text:component(field('OBR',4),2)}},false);
    } else if(family==='ORU'){
        var order=labID(field('OBR',2));if(bad&&cfg.defect==='link')order='lab-wrong-order';var results=[];
        var count=lines.filter(function(line){return line.substring(0,3)==='OBX';}).length;
        for(var n=1;n<=count;n++){var oid='lab-observation-'+field('OBX',1,n);var status=field('OBX',11,n)==='C'?'corrected':field('OBX',11,n)==='F'?'final':'preliminary';store({resourceType:'Observation',id:oid,status:status,code:{text:component(field('OBX',3,n),2)},subject:{reference:'Patient/'+patient},basedOn:[{reference:'ServiceRequest/'+order}],valueString:field('OBX',5,n)},false);results.push({reference:'Observation/'+oid});}
        store({resourceType:'DiagnosticReport',id:labID(field('OBR',3)),status:'final',code:{text:component(field('OBR',4),2)},subject:{reference:'Patient/'+patient},basedOn:[{reference:'ServiceRequest/'+order}],result:results},false);
    } else {throw 'unsupported synthetic family';}
}
return 'independent lab route completed';
