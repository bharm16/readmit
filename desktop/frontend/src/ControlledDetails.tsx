import { useState, type ReactNode } from "react";

// A disclosure whose open state this window owns. The native <details> and
// its summary carry the pointer, keyboard and assistive-technology behaviour;
// the state drives the open attribute so a programmatic opener, the summary's
// own toggle and the keyboard all reach the same place instead of racing the
// browser's internal flag.
export function ControlledDetails({
  summary,
  children,
  className,
  initialOpen = false,
}: {
  summary: string;
  children: ReactNode;
  className?: string;
  initialOpen?: boolean;
}) {
  const [open, setOpen] = useState(initialOpen);
  return (
    <details
      className={className}
      open={open}
      onToggle={(event) => setOpen((event.target as HTMLDetailsElement).open)}
    >
      <summary
        onClick={(event) => {
          event.preventDefault();
          setOpen(!open);
        }}
      >
        {summary}
      </summary>
      {children}
    </details>
  );
}
