import { GoogleGenAI } from "@google/genai";
import { NextRequest, NextResponse } from "next/server";

export const runtime = "nodejs";

const identifySchema = {
  type: "object",
  properties: {
    object_name: { type: "string" },
    confidence_score: { type: "number" },
    summary: { type: "string" },
    key_details: {
      type: "array",
      items: { type: "string" },
    },
    suggested_actions: {
      type: "array",
      items: { type: "string" },
    },
  },
  required: [
    "object_name",
    "confidence_score",
    "summary",
    "key_details",
    "suggested_actions",
  ],
} as const;

const PROMPT = `Analyze the uploaded image and identify the primary object or subject.
Return structured JSON only with:
- object_name: concise name of the main object
- confidence_score: number from 0 to 1
- summary: 1-2 sentence description
- key_details: notable visual characteristics
- suggested_actions: practical next steps related to the object`;

export async function POST(request: NextRequest) {
  try {
    const apiKey = process.env.GEMINI_API_KEY;
    if (!apiKey) {
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "MISSING_API_KEY",
            message: "GEMINI_API_KEY is not configured",
          },
        },
        { status: 500 },
      );
    }

    const formData = await request.formData();
    const image = formData.get("image");

    if (!(image instanceof File)) {
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "MISSING_IMAGE",
            message: "No image provided",
          },
        },
        { status: 400 },
      );
    }

    const bytes = Buffer.from(await image.arrayBuffer()).toString("base64");
    const mimeType = image.type || "image/jpeg";

    const ai = new GoogleGenAI({ apiKey });
    const response = await ai.models.generateContent({
      model: "gemini-3.1-flash-lite",
      contents: [
        {
          inlineData: {
            mimeType,
            data: bytes,
          },
        },
        { text: PROMPT },
      ],
      config: {
        responseMimeType: "application/json",
        responseJsonSchema: identifySchema,
      },
    });

    const text = response.text;
    if (!text) {
      return NextResponse.json(
        {
          success: false,
          error: {
            code: "EMPTY_MODEL_RESPONSE",
            message: "Model returned an empty response",
          },
        },
        { status: 502 },
      );
    }

    const parsed = JSON.parse(text) as {
      object_name: string;
      confidence_score: number;
      summary: string;
      key_details: string[];
      suggested_actions: string[];
    };

    return NextResponse.json({
      success: true,
      data: parsed,
    });
  } catch (error) {
    const message =
      error instanceof Error ? error.message : "Failed to identify object";
    return NextResponse.json(
      {
        success: false,
        error: {
          code: "IDENTIFY_FAILED",
          message,
        },
      },
      { status: 500 },
    );
  }
}
