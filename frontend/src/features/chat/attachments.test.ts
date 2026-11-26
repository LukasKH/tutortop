// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { attach, MAX_IMAGE_BYTES, MAX_IMAGES } from "@/features/chat/attachments";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function file(name: string, type: string, bytes: number | string = "picture") {
  return new File([typeof bytes === "string" ? bytes : new Uint8Array(bytes)], name, { type });
}

describe("attach", () => {
  test.each(["image/png", "image/jpeg", "image/gif", "image/webp"])("takes %s, as base64 for the app", async (type) => {
    const { attached, errors } = await attach([file("shot", type, "pixels")], MAX_IMAGES);
    expect(errors).toEqual([]);
    expect(attached).toHaveLength(1);
    expect(atob(attached[0].data)).toBe("pixels");
    expect(attached[0].url).toBe(`data:${type};base64,${attached[0].data}`);
  });

  test("refuses other files, naming them, and keeps the images", async () => {
    const { attached, errors } = await attach(
      [file("notes.pdf", "application/pdf"), file("logo.svg", "image/svg+xml"), file("a.png", "image/png")],
      MAX_IMAGES,
    );
    expect(attached.map((a) => a.name)).toEqual(["a.png"]);
    expect(errors).toEqual(["notes.pdf isn't a PNG, JPEG, GIF or WebP image.", "logo.svg isn't a PNG, JPEG, GIF or WebP image."]);
  });

  test("attaches only as many as there's room for", async () => {
    const { attached, errors } = await attach([file("a.png", "image/png"), file("b.png", "image/png"), file("c.png", "image/png")], 2);
    expect(attached.map((a) => a.name)).toEqual(["a.png", "b.png"]);
    expect(errors).toEqual([`A message can have at most ${MAX_IMAGES} images.`]);
  });

  test("refuses a big GIF rather than stop its animation", async () => {
    const { attached, errors } = await attach([file("demo.gif", "image/gif", 6 * 1024 * 1024)], MAX_IMAGES);
    expect(attached).toEqual([]);
    expect(errors).toEqual(["demo.gif is 6 MB; images can be at most 5 MB."]);
  });

  test("scales a big screenshot down to fit", async () => {
    vi.stubGlobal("createImageBitmap", async () => ({ width: 5120, height: 2880, close: () => {} }));
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({ drawImage: () => {} } as unknown as CanvasRenderingContext2D);
    const sizes: number[][] = [];
    vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(function (this: HTMLCanvasElement, done, type) {
      sizes.push([this.width, this.height]);
      done(new Blob(["smaller"], { type }));
    });
    const { attached, errors } = await attach([file("screen.png", "image/png", MAX_IMAGE_BYTES + 1)], MAX_IMAGES);
    expect(errors).toEqual([]);
    expect(sizes).toEqual([[2048, 1152]]);
    expect(atob(attached[0].data)).toBe("smaller");
  });

  test("refuses a big image it can't scale down", async () => {
    vi.stubGlobal("createImageBitmap", async () => Promise.reject(new Error("can't decode")));
    const { attached, errors } = await attach([file("", "image/png", MAX_IMAGE_BYTES + 1)], MAX_IMAGES);
    expect(attached).toEqual([]);
    expect(errors).toEqual(["The pasted image is 5.1 MB; images can be at most 5 MB."]);
  });
});
