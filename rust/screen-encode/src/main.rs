// Screen frames are polled every few seconds. A full-resolution JPEG per poll
// costs tens of megabytes a minute for a thumbnail-sized panel, so this tool
// takes the raw frame on stdin and writes one smaller JPEG on stdout.
//
// It is a pure filter: no file paths, no network, no state. The Go server falls
// back to the untouched frame when this binary is missing or fails, so it is an
// optimization and never a dependency of the capture path.
use std::env;
use std::io::{self, Read, Write};

use image::codecs::jpeg::JpegEncoder;
use image::imageops::FilterType;
use image::{ExtendedColorType, ImageFormat};

const DEFAULT_MAX_WIDTH: u32 = 1280;
const DEFAULT_QUALITY: u8 = 55;

struct Options {
    max_width: u32,
    quality: u8,
}

fn parse_options() -> Result<Options, String> {
    let mut options = Options {
        max_width: DEFAULT_MAX_WIDTH,
        quality: DEFAULT_QUALITY,
    };
    let mut args = env::args().skip(1);
    while let Some(flag) = args.next() {
        match flag.as_str() {
            "--max-width" => {
                let value = args.next().ok_or("--max-width needs a value")?;
                let width = value.parse::<u32>().map_err(|_| "--max-width must be a number")?;
                if width == 0 || width > 8192 {
                    return Err("--max-width must be between 1 and 8192".into());
                }
                options.max_width = width;
            }
            "--quality" => {
                let value = args.next().ok_or("--quality needs a value")?;
                let quality = value.parse::<u8>().map_err(|_| "--quality must be a number")?;
                if quality == 0 || quality > 100 {
                    return Err("--quality must be between 1 and 100".into());
                }
                options.quality = quality;
            }
            "--help" | "-h" => {
                return Err("usage: screen-encode [--max-width N] [--quality N] < frame.jpg > frame.jpg".into());
            }
            other => return Err(format!("unknown argument: {other}")),
        }
    }
    Ok(options)
}

fn shrink(frame: &[u8], options: &Options) -> Result<Vec<u8>, String> {
    let decoded = image::load_from_memory_with_format(frame, ImageFormat::Jpeg)
        .map_err(|error| format!("decode jpeg: {error}"))?;
    let (width, height) = (decoded.width(), decoded.height());
    if width == 0 || height == 0 {
        return Err("frame has no pixels".into());
    }
    let resized = if width > options.max_width {
        let target_height = ((height as u64 * options.max_width as u64) / width as u64).max(1) as u32;
        decoded.resize_exact(options.max_width, target_height, FilterType::Triangle)
    } else {
        decoded
    };
    let rgb = resized.to_rgb8();
    let mut encoded = Vec::with_capacity(frame.len() / 2);
    let mut encoder = JpegEncoder::new_with_quality(&mut encoded, options.quality);
    encoder
        .encode(rgb.as_raw(), rgb.width(), rgb.height(), ExtendedColorType::Rgb8)
        .map_err(|error| format!("encode jpeg: {error}"))?;
    Ok(encoded)
}

fn main() {
    let options = match parse_options() {
        Ok(options) => options,
        Err(message) => {
            let _ = writeln!(io::stderr(), "{message}");
            std::process::exit(2);
        }
    };
    let mut frame = Vec::new();
    if let Err(error) = io::stdin().read_to_end(&mut frame) {
        let _ = writeln!(io::stderr(), "read stdin: {error}");
        std::process::exit(1);
    }
    match shrink(&frame, &options) {
        Ok(output) => {
            if let Err(error) = io::stdout().write_all(&output) {
                let _ = writeln!(io::stderr(), "write stdout: {error}");
                std::process::exit(1);
            }
        }
        Err(error) => {
            let _ = writeln!(io::stderr(), "{error}");
            std::process::exit(1);
        }
    }
}
