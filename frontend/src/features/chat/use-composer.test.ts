// @vitest-environment jsdom
import { afterEach, expect, test } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { useComposer } from "@/features/chat/use-composer";
import { MAX_IMAGES } from "@/features/chat/attachments";

afterEach(cleanup);

const png = (name: string) => new File([name], name, { type: "image/png" });

test("keeps the images it can take, says why it refused the rest, and sends them with the text", async () => {
  const { result } = renderHook(() => useComposer());
  await act(() => result.current.addImages([png("a.png"), new File(["x"], "notes.txt", { type: "text/plain" })]));
  expect(result.current.images.map((i) => i.name)).toEqual(["a.png"]);
  expect(result.current.imageError).toBe("notes.txt isn't a PNG, JPEG, GIF or WebP image.");

  // Adding past the limit keeps the first ones.
  await act(() => result.current.addImages(["b", "c", "d", "e"].map((n) => png(`${n}.png`))));
  expect(result.current.images.map((i) => i.name)).toEqual(["a.png", "b.png", "c.png", "d.png"]);
  expect(result.current.imageError).toBe(`A message can have at most ${MAX_IMAGES} images.`);

  act(() => result.current.removeImage(result.current.images[0].id));
  expect(result.current.images.map((i) => i.name)).toEqual(["b.png", "c.png", "d.png"]);
  expect(result.current.imageError).toBe("");

  act(() => result.current.setDraft("  what's wrong here?  "));
  let sent: ReturnType<typeof result.current.take> | undefined;
  act(() => {
    sent = result.current.take();
  });
  expect(sent).toEqual({ text: "what's wrong here?", images: [btoa("b.png"), btoa("c.png"), btoa("d.png")] });
  expect(result.current.draft).toBe("");
  expect(result.current.images).toEqual([]);
});
