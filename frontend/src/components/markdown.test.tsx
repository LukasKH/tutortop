// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { Markdown } from "@/components/markdown";

// Monaco needs a real browser; with no languages, code stays plain text.
vi.mock("@/lib/monaco", () => ({
  monaco: { editor: { colorize: async (code: string) => code }, languages: { getLanguages: () => [] } },
}));

afterEach(cleanup);

const show = (text: string, props: { foldExamples?: boolean } = {}) => render(<Markdown text={text} {...props} />).container;

// The TeX KaTeX drew each formula from, in order, and whether it was drawn as a block.
const formulas = (root: HTMLElement) =>
  [...root.querySelectorAll(".katex")].map((el) => ({
    tex: el.querySelector('annotation[encoding="application/x-tex"]')?.textContent?.trim(),
    display: el.parentElement?.classList.contains("katex-display") ?? false,
  }));

describe("Markdown math", () => {
  test.each([
    ["inline $…$", "Lookup is $O(1)$ on average.", [{ tex: "O(1)", display: false }]],
    ["$…$ starting with a digit", "Answer modulo $10^9 + 7$.", [{ tex: "10^9 + 7", display: false }]],
    ["two formulas in a sentence", "For $a$ and $b$, $a+b$.", ["a", "b", "a+b"].map((tex) => ({ tex, display: false }))],
    ["$$…$$ inside a sentence", "The sum $$\\sum_i x_i$$ grows.", [{ tex: "\\sum_i x_i", display: false }]],
    ["$$…$$ as its own paragraph", "Total:\n\n$$\\sum_i x_i$$", [{ tex: "\\sum_i x_i", display: true }]],
    ["$$ on lines of their own", "$$\n\\sum_i x_i\n$$", [{ tex: "\\sum_i x_i", display: true }]],
    ["```math", "```math\n\\sum_i x_i\n```", [{ tex: "\\sum_i x_i", display: true }]],
    [
      "math in a list",
      "- $x^2$\n- $y$",
      [
        { tex: "x^2", display: false },
        { tex: "y", display: false },
      ],
    ],
    ["math in a table", "| cost |\n| --- |\n| $O(n)$ |", [{ tex: "O(n)", display: false }]],
  ])("draws %s", (_, text, want) => {
    expect(formulas(show(text))).toEqual(want);
  });

  test.each([
    ["two prices", "It costs $5 and $10."],
    ["a price range", "Between $5-$10 per month."],
    ["prices with commas", "From $20,000 to $30,000."],
    ["a dollar before a space", "Pay $ 5 now and $ 6 later."],
    ["a dollar after a space", "Bring 5$ and 6 $ more."],
    ["an escaped dollar", "Both \\$x\\$ and \\$y\\$."],
    ["a lone dollar", "Prices start at $5."],
  ])("leaves %s as prose", (_, text) => {
    const root = show(text);
    expect(formulas(root)).toEqual([]);
    expect(root.textContent).toBe(text.replaceAll("\\$", "$"));
  });

  test("keeps the formula after prices in the same paragraph", () => {
    const root = show("It costs $5 and $10, so $x = 15$ in **total**.");
    expect(formulas(root)).toEqual([{ tex: "x = 15", display: false }]);
    expect(root.querySelector("p")?.firstChild?.textContent).toBe("It costs $5 and $10, so ");
    expect(root.querySelector("strong")?.textContent).toBe("total");
  });

  test("keeps markdown between prices", () => {
    const root = show("Was $5 and **now** $10.");
    expect(root.querySelector("strong")?.textContent).toBe("now");
    expect(root.textContent).toBe("Was $5 and now $10.");
  });

  test.each([
    ["inline code", "Write `$O(1)$` and `$$x$$`.", ["$O(1)$", "$$x$$"]],
    ["inline code after prices", "It costs $5 and $10, and `$O(1)$` stays code.", ["$O(1)$"]],
    ["a fenced block", "```python\nprint(f'$x$ costs $5')\ny = '$$z$$'\n```", ["print(f'$x$ costs $5')\ny = '$$z$$'"]],
    ["a fence with no language", "```\n$$\n\\sum_i x_i\n$$\n```", ["$$\n\\sum_i x_i\n$$"]],
  ])("leaves math in %s literal", (_, text, codes) => {
    const root = show(text);
    expect(formulas(root)).toEqual([]);
    expect([...root.querySelectorAll("code")].map((c) => c.textContent)).toEqual(codes);
  });

  test("shows a formula that doesn't parse as its source", () => {
    const root = show("Half written: $\\frac{1}{$");
    expect(root.querySelector(".katex-error")?.textContent).toBe("\\frac{1}{");
  });

  test("draws math in a task statement's examples", () => {
    const root = show("## Example 1\n\n**Input:** $n = 3$\n\n## Background\n\n```python\n# Cost\nx = '$5'\n```", {
      foldExamples: true,
    });
    expect(formulas(root)).toEqual([{ tex: "n = 3", display: false }]);
    expect(root.querySelector(".example-io .katex")).not.toBeNull();
    expect(root.querySelector("details.example summary")?.textContent).toBe("Cost");
    expect(root.querySelector("details.example code")?.textContent).toBe("x = '$5'");
  });
});
