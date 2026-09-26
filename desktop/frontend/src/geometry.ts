// The layout lengths the window's code decides with, in rem. styles.css holds
// the same tokens for drawing; styles.test.ts fails if the two disagree, so a
// breakpoint computed here always matches what the stylesheet draws.

/** The labelled sidebar (--sidebar). */
export const SIDEBAR_REM = 13;
/** The icon rail that replaces it in a narrow window (--icon-rail). */
export const ICON_RAIL_REM = 3.25;
/** Below this effective window width the sidebar is the icon rail. */
export const RAIL_BREAKPOINT_REM = 56.25;
/** One table row (--row). */
export const ROW_REM = 2.75;
/** The details beside a list (--inspector): preferred, and the resizable range. */
export const INSPECTOR_REM = 22.5;
export const INSPECTOR_MIN_REM = 20;
export const INSPECTOR_MAX_REM = 27.5;
/** The narrowest useful list beside the details; below it the details show alone. */
export const LIST_MIN_REM = 30;
/** Categories sit in a rail beside their page only where the page is this wide. */
export const CATEGORY_RAIL_MIN_REM = 45;
