/**
Check if there are images on the clipboard.

@returns A promise that resolves to `true` if there are images on the clipboard, `false` otherwise. On non-macOS platforms, it returns `false`.

@example
```
import {hasClipboardImages} from 'clipboard-image';

if (await hasClipboardImages()) {
	console.log('Images found on clipboard');
}
```
*/
export function hasClipboardImages(): Promise<boolean>;

/**
Read images from the clipboard and save them as PNG files in a unique temporary directory.

You get PNG files no matter what image types they were on the clipboard to make it easier to handle.

@returns A promise that resolves to an array of file paths to the saved PNG files. You are in charge of these files. You can move them somewhere else or clean them up when you are done. On non-macOS platforms, it returns an empty array.

@example
```
import {readClipboardImages} from 'clipboard-image';

const files = await readClipboardImages();
//=> ['/var/folders/.../clipboard-image-1.png', '/var/folders/.../clipboard-image-2.png']
```
*/
export function readClipboardImages(): Promise<string[]>;

/**
Write images to the clipboard.

Supports any image type that macOS supports, which includes PNG, JPEG, HEIC, WebP, GIF.

On non-macOS platforms, it does nothing.

@param filePaths - An array of file paths and file URL objects pointing to image files.

@example
```
import {writeClipboardImages} from 'clipboard-image';

await writeClipboardImages(['screenshot.png', 'photo.jpg']);
```
*/
export function writeClipboardImages(filePaths: ReadonlyArray<string | URL>): Promise<void>;

