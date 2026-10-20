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
				// The landing page's one line on what the latest release added. The
				// version beside it comes from plugin.json, which each release bumps.
				release: z.string().optional(),
			}),
		}),
	}),
};
