// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Diagram } from "@/components/diagram";

// Mermaid needs a real layout engine, so this fake draws "graph" sources and rejects the rest.
vi.mock("mermaid", () => ({
  default: {
    initialize: () => {},
    render: async (id: string, code: string) => {
      if (!code.startsWith("graph")) throw new Error("parse error");
      return { svg: `<svg id="${id}" data-testid="drawing"><text>${code}</text></svg>` };
    },
  },
}));

afterEach(cleanup);

describe("a drawn diagram", () => {
  test("opens large on click and closes on Escape", async () => {
    const user = userEvent.setup();
    render(<Diagram code="graph LR; A-->B" />);

    await user.click(await screen.findByRole("button", { name: "Expand diagram" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog.querySelector("svg")).not.toBeNull();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  test("closes on a click outside", async () => {
    const user = userEvent.setup();
    render(<Diagram code="graph LR; A-->B" />);

    await user.click(await screen.findByRole("button", { name: "Expand diagram" }));
    await user.click(document.querySelector("[data-slot=dialog-overlay]")!);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  test.each(["{Enter}", " "])("opens from the keyboard with %j", async (key) => {
    const user = userEvent.setup();
    render(<Diagram code="graph LR; A-->B" />);

    (await screen.findByRole("button", { name: "Expand diagram" })).focus();
    await user.keyboard(key);
    expect(screen.getByRole("dialog")).not.toBeNull();
  });
});

describe("a diagram that isn't drawn", () => {
  test("shows its source without an expand button while pending", () => {
    render(<Diagram code="graph LR; A-->B" />);
    expect(screen.getByText("graph LR; A-->B")).not.toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  test("shows its source without an expand button when it doesn't parse", async () => {
    const { container } = render(<Diagram code="not a diagram" />);
    await vi.waitFor(() => expect(container.querySelector("pre.diagram-pending")).toBeNull());
    expect(screen.getByText("not a diagram")).not.toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
