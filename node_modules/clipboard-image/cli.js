#!/usr/bin/env node
import process from 'node:process';
import {readClipboardImages, writeClipboardImages} from './index.js';

const arguments_ = process.argv.slice(2);

if (arguments_.length > 0) {
	await writeClipboardImages(arguments_);
	const plural = arguments_.length > 1 ? 's' : '';
	console.log(`${arguments_.length} image${plural} copied to clipboard`);
} else {
	const files = await readClipboardImages();
	if (files.length === 0) {
		console.error('No images found on clipboard');
		process.exit(1);
	}

	console.log(files.join('\n'));
}
