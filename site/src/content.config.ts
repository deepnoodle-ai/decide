import { defineCollection } from 'astro:content';
import { z } from 'astro/zod';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

export const collections = {
	docs: defineCollection({
		loader: docsLoader(),
		schema: docsSchema({
			extend: z.object({
				// The page's answer line for its link preview, such as "! severe  yes".
				// A verdict, never a percentage. A leading "!" draws it as flagged.
				answer: z.string().optional(),
			}),
		}),
	}),
};
