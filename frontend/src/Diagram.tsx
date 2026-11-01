import { useEffect, useId, useState } from "react";

// Mermaid diagrams in lessons and chat, drawn in the app's greys. Mermaid is
// large, so it loads the first time a diagram is shown.
let ready: Promise<typeof import("mermaid").default> | null = null;

function loadMermaid() {
  ready ??= import("mermaid").then(({ default: mermaid }) => {
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
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

/** A diagram, or its source as code when it doesn't parse (for example while the coach is still writing it). */
export function Diagram({ code }: { code: string }) {
  const id = `diagram-${useId().replace(/[^a-zA-Z0-9]/g, "")}`;
  const [svg, setSvg] = useState("");
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let live = true;
    loadMermaid()
      .then((mermaid) => mermaid.render(id, code))
      .then((r) => live && (setSvg(r.svg), setFailed(false)))
      .catch(() => {
        live && setFailed(true);
        // A failed render leaves its scratch element behind.
        document.getElementById(`d${id}`)?.remove();
      });
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
  return <figure className="diagram" dangerouslySetInnerHTML={{ __html: svg }} />;
}
