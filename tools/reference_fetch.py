"""Retain the requested HL7 Europe edition chapters for explicit local extraction.

Development/setup command only; no network code enters the desktop or engine.
It follows observed chapter links within each edition and records exact hashes.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
import re
import subprocess
from urllib.parse import urljoin, urlparse

INDEX = "https://hl7.eu/HL7v2x/hl7contents.htm"
EDITIONS = ("2.1", "2.2", "2.3", "2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7", "2.7.1", "2.8", "2.8.1", "2.8.2", "2.9")


class Links(HTMLParser):
    def __init__(self):
        super().__init__()
        self.links = []

    def handle_starttag(self, tag, attrs):
        if tag == "a" and dict(attrs).get("href"):
            self.links.append(dict(attrs)["href"])


def links(raw):
    parsed = Links()
    parsed.feed(raw.decode("utf-8", errors="replace"))
    return parsed.links


def fetch(url, output):
    parsed = urlparse(url)
    if parsed.scheme != "https" or parsed.hostname != "hl7.eu" or not parsed.path.startswith("/HL7v2x/"):
        raise ValueError("reference source is outside the declared publisher site")
    output.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if not output.exists():
        temporary = output.with_suffix(output.suffix + ".incomplete")
        subprocess.run(["curl", "--fail", "--silent", "--show-error", "--location", "--proto", "=https", "--proto-redir", "=https", "--max-time", "90", "--max-filesize", "16777216", "--output", str(temporary), url], check=True)
        temporary.rename(output)
    body = output.read_bytes()
    return {"file": output.name, "url": url, "sha256": hashlib.sha256(body).hexdigest()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--official-sources",type=Path)
    args = parser.parse_args()
    fetch(INDEX, args.output / "index.html")
    observed = links((args.output / "index.html").read_bytes())
    inventory = {"schema": "readmit-reference-html-sources/v1", "publisher_index": INDEX, "editions": {}}
    for edition in EDITIONS:
        if args.official_sources:
            import profile_hl7 as hl7
            if edition in hl7.SOURCES and all((args.official_sources/name).is_file() for role,name in hl7.SOURCES[edition].items() if role in ("schemas","standard")):
                inventory["editions"][edition]=[]
                continue
        code = edition.replace(".", "")
        candidates = [link for link in observed if re.fullmatch("v" + code + "/std" + code + r"/hl7\.html?", link)]
        if len(candidates) != 1:
            raise ValueError("publisher index has no unique edition " + edition)
        index = urljoin(INDEX, candidates[0])
        fetch(index, args.output / edition / "contents.html")
        chapter_urls = sorted({urljoin(index, link.split("#")[0]) for link in links((args.output / edition / "contents.html").read_bytes()) if re.fullmatch(r"(?:ch|kap|HL7CHP)\d+[abc]?\.html?", link.split("#")[0], re.I)})
        if not chapter_urls:
            raise ValueError("edition has no observed chapter links: " + edition)
        observed_chapter_urls=list(chapter_urls)
        # The published legacy TOCs use lower-case links for upper-case files.
        if edition in ("2.1", "2.2"):
            prefix="KAP" if edition=="2.1" else "HL7CHP"
            chapter_urls=[re.sub(r"/(?:kap|hl7chp)(\d+)\.html$",lambda m:"/"+prefix+m.group(1)+".html",url,flags=re.I) for url in chapter_urls]
        if edition=="2.9":
            chapter_urls=[re.sub(r"(ch\d+)([abc])(\.html)$",lambda m:m.group(1)+m.group(2).upper()+m.group(3),url) for url in chapter_urls]
        repairs={resolved:original for original,resolved in zip(observed_chapter_urls,chapter_urls) if original!=resolved}
        with ThreadPoolExecutor(max_workers=4) as pool:
            entries=[]
            futures=[(url,pool.submit(fetch,url,args.output/edition/urlparse(url).path.rsplit("/",1)[-1])) for url in chapter_urls]
            for url,future in futures:
                try:entries.append(future.result())
                except subprocess.CalledProcessError:
                    inventory.setdefault("unavailable",{}).setdefault(edition,[]).append(url)
            if not entries:raise ValueError("edition has no readable chapter sources: "+edition)
        for entry in entries:
            if entry["url"] in repairs:
                entry["observed_url"]=repairs[entry["url"]];entry["repair"]="Canonical publisher filename case"
        if edition in ("2.1","2.9"):
            root=urljoin(INDEX,"v"+code+"/hl7v"+code+".htm")
            fetch(root,args.output/edition/"database.html")
            raw=(args.output/edition/"database.html").read_text(errors="replace")
            database_observed=re.findall(r"window.location.href\s*=\s*'([^']+)'",raw)
            table_index=next((link for link in database_observed if re.fullmatch("hl7v"+code+r"tab\.htm",link)),None)
            if table_index:
                tables=urljoin(root,table_index);fetch(tables,args.output/edition/"tables.html")
                table_urls=sorted({urljoin(tables,link) for link in links((args.output/edition/"tables.html").read_bytes()) if re.fullmatch("hl7v"+code+r"tab\d{4}\.htm",link)})
                with ThreadPoolExecutor(max_workers=4) as pool:
                    for url,result in zip(table_urls,pool.map(lambda url:fetch(url,args.output/edition/urlparse(url).path.rsplit("/",1)[-1]),table_urls)):
                        entries.append(result)
        inventory["editions"][edition] = entries
        (args.output / "sources.json").write_text(json.dumps(inventory, indent=2))
        print(json.dumps({"edition": edition, "chapters": len(entries)}), flush=True)


if __name__ == "__main__":
    main()
