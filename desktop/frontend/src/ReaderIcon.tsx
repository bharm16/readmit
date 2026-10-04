import expand from "./assets/parser/expand.svg";
import previous from "./assets/parser/previous.svg";
import next from "./assets/parser/next.svg";
import position from "./assets/parser/position.svg";
import components from "./assets/parser/components.svg";
import copy from "./assets/parser/copy.svg";
import overview from "./assets/parser/overview.svg";
import componentTab from "./assets/parser/component-tab.svg";
import dataElement from "./assets/parser/data-element.svg";
import disclosure from "./assets/parser/disclosure.svg";
import book from "./assets/parser/book.svg";
import showMore from "./assets/parser/show-more.svg";
import settings from "./assets/parser/settings.svg";
import close from "./assets/parser/close.svg";

const icons = { expand, previous, next, position, components, copy, overview, componentTab, dataElement, disclosure, book, showMore, settings, close };

/** Local Figma assets keep their exported geometry inside a fixed icon slot. */
export function ReaderIcon({ name }: { name: keyof typeof icons }) {
  return <span className="reader-icon" aria-hidden="true"><img src={icons[name]} alt="" /></span>;
}
