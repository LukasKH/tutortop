import { useEffect, useId, useState } from "react";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog";

/**
 * A diagram, or its source as code when it doesn't parse (for example while the
 * tutor is still writing it). Clicking a drawn diagram opens it as large as the window.
 */
export function Diagram({ code }: { code: string }) {
  const id = `diagram-${useId().replace(/[^a-zA-Z0-9]/g, "")}`;
  const [svg, setSvg] = useState("");
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let live = true;
    loadMermaid()
      .then((mermaid) => mermaid.render(id, code))
      .then((r) => live && (setSvg(r.svg), setFailed(false)))
      .catch(() => live && setFailed(true));
    return () => {
      live = false;
    };
  }, [id, code]);

  if (failed || !svg) {
    return (
      <pre className={failed ? "" : "diagram-pending"}>
        <code>{code}</code>
      </pre>
    );
  }
  // Mermaid sanitises the output (securityLevel "strict").
  const drawing = { __html: svg };
  return (
    <Dialog>
      {/* The button sits inside the figure: a button may hold an svg but not a figure. */}
      <figure className="diagram">
        <DialogTrigger asChild>
          <button
            type="button"
            aria-label="Expand diagram"
            className="flex w-full cursor-zoom-in justify-center rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
            dangerouslySetInnerHTML={drawing}
          />
        </DialogTrigger>
      </figure>
      <DialogContent aria-describedby={undefined} className="h-[calc(100vh-4rem)] max-w-[calc(100vw-4rem)] bg-background p-8 sm:max-w-[calc(100vw-4rem)]">
        <DialogTitle className="sr-only">Diagram</DialogTitle>
        {/* Mermaid sets the svg's max-width inline to its drawn size; lift it so the diagram fills the window. */}
        <div className="min-h-0 [&_svg]:size-full [&_svg]:max-w-none!" dangerouslySetInnerHTML={drawing} />
      </DialogContent>
    </Dialog>
  );
}

// Mermaid diagrams in lessons and chat, drawn in the app's greys. Mermaid is
// large, so it loads the first time a diagram is shown.
let ready: Promise<typeof import("mermaid").default> | null = null;

function loadMermaid() {
  ready ??= import("mermaid").then(({ default: mermaid }) => {
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      // A diagram that fails throws and leaves nothing behind; the code shows instead.
      suppressErrorRendering: true,
      theme: "base",
      fontFamily: '"Atkinson Hyperlegible Next Variable", ui-sans-serif, system-ui, sans-serif',
      themeVariables: {
        darkMode: true,
        background: "transparent",
        fontSize: "13px",
        primaryColor: "#161616",
        primaryTextColor: "#ededed",
        primaryBorderColor: "#3a3a3a",
        secondaryColor: "#1c1c1c",
        tertiaryColor: "#111111",
        lineColor: "#8a8a8a",
        textColor: "#bdbdbd",
        noteBkgColor: "#1c1c1c",
        noteTextColor: "#bdbdbd",
        noteBorderColor: "#3a3a3a",
        actorBkg: "#161616",
        actorBorder: "#3a3a3a",
        actorTextColor: "#ededed",
        signalColor: "#bdbdbd",
        signalTextColor: "#bdbdbd",
      },
    });
    return mermaid;
  });
  return ready;
}
