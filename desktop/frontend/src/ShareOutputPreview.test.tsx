import {test,expect} from "vitest";
import {render,screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {ShareOutputPreview} from "./ShareReport";

test("message outputs named HTML remain exact literal text while actual report HTML stays inert",async()=>{
 const user=userEvent.setup();
 const text="<p>Retained fixture source</p>";
 const shown=render(<ShareOutputPreview files={[{name:"source.html",kind:"message",size:text.length,text},{name:"report.html",kind:"report",size:text.length,text}]}/>);
 expect(screen.getByLabelText("source.html",{selector:"pre"}).textContent).toBe(text);
 expect(shown.container.querySelector("iframe")).toBeNull();
 await user.click(screen.getByRole("tab",{name:"report.html"}));
 const document=screen.getByTitle("report.html");
 expect(document.getAttribute("sandbox")).toBe("");
 expect(document.getAttribute("srcdoc")).toBe(text);
});

test("binary message output named PDF stays an exact hexadecimal preview",()=>{
 const shown=render(<ShareOutputPreview files={[{name:"source.pdf",kind:"message",size:3,data:"AAEC"}]}/>);
 expect(screen.getByLabelText("source.pdf").textContent).toBe("00 01 02");
 expect(shown.container.querySelector("iframe")).toBeNull();
});
