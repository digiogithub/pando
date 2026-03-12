/* globals $, ObjC */
/* eslint-disable new-cap */
import process from 'node:process';
import {fileURLToPath} from 'node:url';
import {runJxa} from 'run-jxa';

export async function hasClipboardImages() {
	if (process.platform !== 'darwin') {
		return false;
	}

	const result = await runJxa(() => {
		ObjC.import('AppKit');
		ObjC.import('Foundation');

		const pasteboard = $.NSPasteboard.generalPasteboard;
		const {imageTypes} = $.NSImage;

		const options = $.NSMutableDictionary.dictionary;
		options.setObjectForKey(
			imageTypes,
			$('NSPasteboardURLReadingContentsConformToTypesKey'),
		);
		options.setObjectForKey(
			$.NSNumber.numberWithBool(true),
			$('NSPasteboardURLReadingFileURLsOnlyKey'),
		);

		const classes = $.NSMutableArray.array;
		classes.addObject($.NSURL);
		classes.addObject($.NSImage);

		const objects = pasteboard.readObjectsForClassesOptions(classes, options);
		return objects && objects.count > 0;
	});

	return result;
}

export async function readClipboardImages() {
	if (process.platform !== 'darwin') {
		return [];
	}

	const result = await runJxa(() => {
		ObjC.import('AppKit');
		ObjC.import('Foundation');

		const pasteboard = $.NSPasteboard.generalPasteboard;
		const {imageTypes} = $.NSImage;

		const options = $.NSMutableDictionary.dictionary;
		options.setObjectForKey(
			imageTypes,
			$('NSPasteboardURLReadingContentsConformToTypesKey'),
		);
		options.setObjectForKey(
			$.NSNumber.numberWithBool(true),
			$('NSPasteboardURLReadingFileURLsOnlyKey'),
		);

		const classes = $.NSMutableArray.array;
		classes.addObject($.NSURL);
		classes.addObject($.NSImage);

		const objects = pasteboard.readObjectsForClassesOptions(classes, options);

		if (!objects || objects.count === 0) {
			return [];
		}

		const fileManager = $.NSFileManager.defaultManager;
		const temporaryRoot = $.NSURL.fileURLWithPathIsDirectory(
			$(ObjC.unwrap($.NSTemporaryDirectory())),
			true,
		);

		const uuid = $.NSUUID.UUID.UUIDString;
		const temporaryDirectory = temporaryRoot.URLByAppendingPathComponent($(uuid));

		fileManager.createDirectoryAtURLWithIntermediateDirectoriesAttributesError(
			temporaryDirectory,
			true,
			$(), // Nil, not NSNull
			$(), // Nil for error
		);

		const resultPaths = [];
		const usedNames = new Set();

		for (let index = 0; index < objects.count; index++) {
			const object = objects.objectAtIndex(index);
			let image = null;
			let filename = `clipboard-image-${index}.png`;

			if (object.isKindOfClass($.NSURL)) {
				image = $.NSImage.alloc.initWithContentsOfURL(object);
				const lastPathComponent = ObjC.unwrap(object.lastPathComponent);
				if (lastPathComponent && lastPathComponent !== '') {
					const nameWithoutExt = lastPathComponent.replace(/\.[^/.]+$/, '');
					filename = `${nameWithoutExt}.png`;
					let counter = 1;
					while (usedNames.has(filename)) {
						filename = `${nameWithoutExt}-${counter}.png`;
						counter++;
					}
				}
			} else if (object.isKindOfClass($.NSImage)) {
				image = object;
			}

			if (!image) {
				continue;
			}

			const tiffData = image.TIFFRepresentation;
			const representation = $.NSBitmapImageRep.imageRepWithData(tiffData);

			if (!representation) {
				continue;
			}

			const pngData = representation.representationUsingTypeProperties(
				$.NSPNGFileType,
				$({}),
			);

			usedNames.add(filename);
			const fileURL = temporaryDirectory.URLByAppendingPathComponent($(filename));
			pngData.writeToURLAtomically(fileURL, true);
			resultPaths.push(ObjC.unwrap(fileURL.path));
		}

		return resultPaths;
	});

	return result;
}

export async function writeClipboardImages(filePaths) {
	if (process.platform !== 'darwin') {
		return;
	}

	// Convert URL objects to file paths
	const paths = filePaths.map(path => path instanceof URL ? fileURLToPath(path) : path);

	// RunJxa passes array elements as separate arguments to the function
	await runJxa((...paths) => {
		ObjC.import('AppKit');
		ObjC.import('Foundation');

		if (paths.length === 0) {
			throw new Error('Expected at least one image path');
		}

		const images = $.NSMutableArray.array;
		for (const imagePath of paths) {
			const url = $.NSURL.fileURLWithPath($(imagePath));
			const image = $.NSImage.alloc.initWithContentsOfURL(url);
			if (!image || image.isNil()) {
				throw new Error('Invalid image file: ' + imagePath);
			}

			images.addObject(image);
		}

		const pasteboard = $.NSPasteboard.generalPasteboard;
		pasteboard.clearContents; // eslint-disable-line no-unused-expressions

		if (pasteboard.writeObjects) {
			pasteboard.writeObjects(images);
		} else {
			// Fallback for older macOS versions
			const firstImage = images.count > 0 ? images.objectAtIndex(0) : undefined;
			if (!firstImage) {
				return;
			}

			const tiffData = firstImage.TIFFRepresentation;
			const representation = $.NSBitmapImageRep.imageRepWithData(tiffData);
			if (!representation) {
				return;
			}

			const pngData = representation.representationUsingTypeProperties(
				$.NSPNGFileType,
				$({}),
			);

			pasteboard.setDataForType(pngData, $('public.png'));
		}
	}, paths);
}
