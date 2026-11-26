/**
 * Images the learner attaches to a message to the tutor, such as a screenshot
 * of an error. The Go app checks the same limits (workspace.MaxChatImages and
 * workspace.MaxChatImageBytes) and reads each image's type from its bytes.
 */

export const IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
export const MAX_IMAGES = 4;
export const MAX_IMAGE_BYTES = 5 * 1024 * 1024;

/** A larger image is scaled down to fit this many pixels on its longer side. Agents scale big images down anyway. */
const MAX_SIDE = 2048;

/** An image waiting in the composer. */
export type Attachment = {
  id: string;
  name: string;
  /** A data: URL, for the thumbnail. */
  url: string;
  /** The image's bytes in base64, as ChatService.SendMessage takes them. */
  data: string;
};

/**
 * Turns picked, pasted or dropped files into attachments, at most `room` of
 * them. Files that aren't a PNG, JPEG, GIF or WebP are refused, and so is an
 * image over MAX_IMAGE_BYTES that can't be scaled down below it (an animated
 * GIF isn't scaled, which would stop it). Each refusal comes with a message
 * for the learner.
 */
export async function attach(files: File[], room: number): Promise<{ attached: Attachment[]; errors: string[] }> {
  const attached: Attachment[] = [];
  const errors: string[] = [];
  for (const file of files) {
    const name = file.name || "The pasted image";
    if (!IMAGE_TYPES.includes(file.type)) {
      errors.push(`${name} isn't a PNG, JPEG, GIF or WebP image.`);
      continue;
    }
    if (attached.length >= room) {
      errors.push(`A message can have at most ${MAX_IMAGES} images.`);
      break;
    }
    let image: Blob = file;
    if (file.size > MAX_IMAGE_BYTES && file.type !== "image/gif") {
      image = await downscale(file).catch(() => file);
    }
    if (image.size > MAX_IMAGE_BYTES) {
      errors.push(`${name} is ${megabytes(file.size)}; images can be at most ${megabytes(MAX_IMAGE_BYTES)}.`);
      continue;
    }
    const url = await dataURL(image);
    attached.push({ id: crypto.randomUUID(), name: file.name, url, data: url.slice(url.indexOf(",") + 1) });
  }
  return { attached, errors };
}

/** Scales an image to fit MAX_SIDE. A JPEG stays a JPEG; anything else becomes a PNG, which keeps a screenshot's text sharp. */
async function downscale(file: File): Promise<Blob> {
  const bitmap = await createImageBitmap(file);
  const scale = Math.min(1, MAX_SIDE / Math.max(bitmap.width, bitmap.height));
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(bitmap.width * scale);
  canvas.height = Math.round(bitmap.height * scale);
  canvas.getContext("2d")!.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  const type = file.type === "image/jpeg" ? "image/jpeg" : "image/png";
  return new Promise((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("couldn't scale the image"))), type, 0.9));
}

function dataURL(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

/** Rounded up, so a file just over the limit doesn't read as the limit. */
function megabytes(bytes: number): string {
  return `${Math.ceil((bytes / (1024 * 1024)) * 10) / 10} MB`;
}
