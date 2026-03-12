# clipboard-image

> Get and set images on the macOS clipboard

## Install

```sh
npm install clipboard-image
```

## Usage

```js
import {
	writeClipboardImages,
	readClipboardImages
} from 'clipboard-image';

await writeClipboardImages(['screenshot.png', 'photo.jpg']);

console.log(await readClipboardImages());
//=> ['/var/folders/.../clipboard-image-1.png', '/var/folders/.../clipboard-image-2.png']
```

## API

### `hasClipboardImages()`

Check if there are images on the clipboard.

Returns a `Promise<boolean>`.

On non-macOS platforms, it returns `false`.

### `readClipboardImages()`

Read images from the clipboard.

You get PNG files no matter what image types they were on the clipboard to make it easier to handle.

Returns a `Promise<string[]>` with paths to the saved PNG files. You are in charge of these files. You can move them somewhere else or clean them up when you are done.

On non-macOS platforms, it returns an empty array.

### `writeClipboardImages(filePaths)`

Write images to the clipboard.

Supports any image type that macOS supports, which includes PNG, JPEG, HEIC, WebP, GIF.

On non-macOS platforms, it does nothing.

#### filePaths

Type: `Array<string | URL>`

An array of file paths and file URL objects pointing to image files.

## CLI

```sh
# Read images from clipboard and output file paths
clipboard-image

# Write images to clipboard
clipboard-image image1.png image2.jpg
```

When reading from clipboard, if there are images, they will be saved to a temporary directory and the file paths will be printed (one per line).

When writing to clipboard, provide one or more image file paths as arguments.

## Related

- [clipboardy](https://github.com/sindresorhus/clipboardy) - Access text on the system clipboard (copy/paste)
- [copy-text-to-clipboard](https://github.com/sindresorhus/copy-text-to-clipboard) - Copy text to the clipboard in the browser
